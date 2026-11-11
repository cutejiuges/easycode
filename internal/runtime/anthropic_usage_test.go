package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/provider/anthropic"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func writeThinkingUsageSample(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"usage-message","model":"claude-test"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"private-signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":%q}}`, text),
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":40,"output_tokens_details":{"thinking_tokens":30}}}`,
		`{"type":"message_stop"}`,
	} {
		fmt.Fprint(w, "data: "+event+"\n\n")
	}
}

func thinkingUsageRuntime(t *testing.T, server *httptest.Server, journal *fakeJournal, display domain.ThinkingDisplay) (*Runtime, provider.Conversation) {
	t.Helper()
	thinking, _ := domain.NewThinkingSettings(domain.ThinkingAdaptive, 0, display)
	settings, _ := domain.NewAnthropicInference(thinking, "high", 16384)
	instance, err := anthropic.New(anthropic.Config{BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "claude-test", Inference: settings})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	conversation, err := instance.NewConversation(runtimeThreadID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testRuntimeConfig(t, journal)
	cfg.ContextProfile, err = contextplan.NewProviderProfile(domain.ProviderAnthropic, "claude-test")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ContextProfile, err = cfg.ContextProfile.WithInferenceSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	cfg.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	r, err := New(conversation, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r, conversation
}

func TestThinkingUsageNativeAndNormalizedShareSyncBoundary(t *testing.T) {
	for _, display := range []domain.ThinkingDisplay{domain.DisplayOmitted, domain.DisplaySummarized} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprint(display, fail), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeThinkingUsageSample(w, "answer") }))
				defer server.Close()
				journal := &fakeJournal{}
				if fail {
					journal.failAt = 3
				}
				r, conversation := thinkingUsageRuntime(t, server, journal, display)
				checked := false
				journal.hook = func(drafts []session.RecordDraft) {
					if len(drafts) < 2 || drafts[1].EventKind() != session.EventSampleUsage {
						return
					}
					checked = true
					if !bytes.Contains(drafts[0].PayloadBytes(), []byte(`"thinking_tokens":{"known":true,"value":30}`)) || !bytes.Contains(drafts[1].PayloadBytes(), []byte(`"reasoning_output":{"state":"known","value":30}`)) {
						t.Error("raw and normalized not in same durable batch")
					}
					if r.cache.snapshot.LastSample != nil {
						t.Error("usage published before Sync")
					}
					view := conversation.ProjectHistory()
					for _, turn := range view.Turns {
						for _, message := range turn.Messages {
							if message.Role == domain.SemanticAssistant {
								t.Error("native sample installed before Sync")
							}
						}
					}
				}
				_, err := collectTurn(r, context.Background(), nil)
				if fail {
					if err == nil || !r.poisoned.Load() || r.cache.snapshot.LastSample != nil {
						t.Fatal("uncertain Sync published usage")
					}
				} else {
					if err != nil || !checked || r.cache.snapshot.LastSample == nil {
						t.Fatal("durable sample missing", err)
					}
					u, _ := r.cache.snapshot.LastSample.Usage.Domain()
					v, k := u.ReasoningOutput().Value()
					if !k || v != 30 {
						t.Fatal("reported thinking lost")
					}
				}
			})
		}
	}
}

func TestThinkingSummaryUsageOnCandidateFailureAndSyncUncertain(t *testing.T) {
	for _, mode := range []string{"success", "no-reduction", "sync-uncertain"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				text := "summary"
				if mode == "no-reduction" && bytes.Contains(body, []byte(`"tool_choice"`)) {
					text = strings.Repeat("large summary ", 1000)
				}
				writeThinkingUsageSample(w, text)
			}))
			defer server.Close()
			journal := &fakeJournal{}
			r, _ := thinkingUsageRuntime(t, server, journal, domain.DisplayOmitted)
			// 普通大输入提供确定的可压缩窗口；同一真实 Provider 持有原生来源。
			if err := r.RunTurn(context.Background(), runtimeInputs(strings.Repeat("history ", 1000)), func(protocol.Event) {}); err != nil {
				t.Fatal(err)
			}
			admission, code := r.preflightCompaction("")
			if code != "" {
				t.Fatal(code)
			}
			if mode == "sync-uncertain" {
				journal.failAt = journal.calls + 2
			}
			var events []protocol.Event
			err := r.runCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", admission, func(e protocol.Event) { events = append(events, e) })
			if mode == "success" {
				if err != nil || r.cache.snapshot.LastSummary == nil {
					t.Fatal("summary usage missing", err)
				}
			} else {
				if err == nil {
					t.Fatal("rejected candidate succeeded")
				}
				failure, decodeErr := protocol.DecodeCompactionFailed(events[len(events)-1])
				if decodeErr != nil || failure.UsageState != "observed" || failure.Usage == nil {
					t.Fatal("trusted terminal usage lost", decodeErr)
				}
				u, _ := failure.Usage.Domain()
				v, k := u.ReasoningOutput().Value()
				if !k || v != 30 {
					t.Fatal("failed summary thinking lost")
				}
				if failure.Durable != (mode == "no-reduction") {
					t.Fatal("incorrect durable claim")
				}
			}
		})
	}
}
