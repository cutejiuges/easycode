package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func assertPlanExecRestore(t *testing.T, live, restored *Conversation) {
	t.Helper()
	full := testAnthropicToolCatalog(t)
	filtered, err := full.ForPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	base := testSamplingInput(t, live.provider.config.Inference)
	if _, err := base.WithPlanMode(full); err == nil {
		t.Fatal("Plan mode accepted the complete catalog")
	}
	if _, err := base.WithToolCatalog(filtered); err == nil {
		t.Fatal("Default mode accepted the restricted catalog")
	}
	ordinaryInput, err := base.WithToolCatalog(full)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := live.compileSamplingRequest(live.history.Entries(), ordinaryInput)
	if err != nil {
		t.Fatal(err)
	}
	var defaultWire messagesRequest
	if err := codec.Unmarshal(plain.Bytes(), &defaultWire); err != nil || defaultWire.System != "" || len(defaultWire.Tools) != 10 {
		t.Fatal("default request changed planning instructions or ordinary tool catalog", err)
	}
	input, err := base.WithPlanMode(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.NewCompactionInput(input, "", 4096); err == nil {
		t.Fatal("compaction accepted ordinary planning sources")
	}
	current, err := live.planSamplingRequest(live.history.Snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restored.planSamplingRequest(restored.history.Snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	a, err := current.evidence()
	if err != nil {
		t.Fatal(err)
	}
	b, err := replayed.evidence()
	if err != nil || a.NativeFingerprint() != b.NativeFingerprint() {
		t.Fatal("restored planning native fingerprint differs", err)
	}
	for _, tc := range []struct {
		mode domain.AnthropicCacheMode
		ttl  string
	}{
		{domain.AnthropicCacheDisabled, ""},
		{domain.AnthropicCache5m, ""},
		{domain.AnthropicCache1h, "1h"},
	} {
		compiled, err := current.compile(tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			System string       `json:"system"`
			Tools  []cachedTool `json:"tools"`
		}
		if err := codec.Unmarshal(compiled.body.Bytes(), &wire); err != nil || wire.System != domain.WorkModePlan.Instructions() || len(wire.Tools) != 7 {
			t.Fatalf("%s lost planning instructions or tool catalog: %v", tc.mode, err)
		}
		for i, item := range wire.Tools {
			wantMarker := tc.mode != domain.AnthropicCacheDisabled && i == len(wire.Tools)-1
			marker := item.CacheControl
			if (marker != nil) != wantMarker || marker != nil && (marker.Type != "ephemeral" || marker.TTL != tc.ttl) {
				t.Fatalf("%s produced an unexpected cache marker on %s: %+v", tc.mode, item.Name, marker)
			}
		}
	}
	cached, err := current.compile(domain.AnthropicCache1h)
	if err != nil {
		t.Fatal(err)
	}
	restoredCache, err := replayed.compile(domain.AnthropicCache1h)
	if err != nil || !cached.body.Equal(restoredCache.body) || cached.body.SHA256() != restoredCache.body.SHA256() {
		t.Fatal("restored planning request bytes or fingerprint differ", err)
	}
	want, err := os.ReadFile("testdata/plan_mode_exec_continuation.golden.json")
	if err != nil || !bytes.Equal(cached.body.Bytes(), bytes.TrimSpace(want)) || cached.markers != 2 {
		t.Fatalf("cached planning request or marker count differs: %v\n%s", err, cached.body.Bytes())
	}
}

func TestPlanProposalUsesCommittedMessagesStopReason(t *testing.T) {
	const markdown = "# 方案\r\n\n步骤\r\n"
	const block = "<proposed_plan>\r\n" + markdown + "</proposed_plan>"
	for _, tc := range []struct {
		name, messageDelta string
		eligible           bool
	}{
		{"end turn", `{"stop_reason":"end_turn"}`, true},
		{"omitted stop reason", `{}`, true},
		{"max tokens with closed markers", `{"stop_reason":"max_tokens"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-plan\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":5}}}\n\n")
				for i, text := range []string{"<proposed_", "plan>\r\n" + markdown + "</proposed_plan>"} {
					fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n", i)
					fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", i, text)
					fmt.Fprintf(w, "data: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", i)
				}
				fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":%s,\"usage\":{\"output_tokens\":4}}\n\ndata: {\"type\":\"message_stop\"}\n\n", tc.messageDelta)
			}))
			defer server.Close()
			instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("plan-fixture"), Model: "claude-test"})
			if err != nil {
				t.Fatal(err)
			}
			defer closeAnthropicProvider(t, instance)
			c := testConversation(t, instance)
			catalog, err := testAnthropicToolCatalog(t).ForPlanMode()
			if err != nil {
				t.Fatal(err)
			}
			input, err := testAnthropicTurnInput(t, c, "Investigate").WithPlanMode(catalog)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := c.Stream(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			terminal := readAnthropicTerminal(t, c, stream)
			if terminal.Kind() != provider.StreamEventCompleted {
				t.Fatal(terminal.Error())
			}
			if err := terminal.PreparedSample().Finalize(); err != nil {
				t.Fatal(err)
			}
			ref := domain.AssistantSampleReference{TurnID: c.historySnapshot()[0].TurnID}
			want := ""
			if tc.eligible {
				want = markdown
			}
			if got, ok := c.ProjectPlanProposal(ref); ok != tc.eligible || got != want {
				t.Fatalf("unexpected proposal: %q, %v", got, ok)
			}
			stopText, err := c.ProjectAssistantSample(ref)
			if err != nil || stopText.Text() != block {
				t.Fatalf("Stop lost its original text projection: %q, %v", stopText.Text(), err)
			}
		})
	}
}
