package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestStreamJSONMatchedPreCompactRequiresLocalTrust(t *testing.T) {
	for _, trigger := range []domain.CompactionTrigger{domain.CompactionManual, domain.CompactionAuto} {
		t.Run(string(trigger), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var samplingCalls, summaryCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if bytes.Contains(body, []byte(`"tool_choice":"none"`)) {
					summaryCalls.Add(1)
				}
				n := samplingCalls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				writeOpenAIAppTurn(w, int(n))
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("stream-compact-fixture")}}
			root, workspace := filepath.Join(privateAppTempDir(t), "sessions"), t.TempDir()
			input := `{"version":1,"type":"context.compact","request_id":"request-1","instructions":"keep test failures"}`
			want := []string{"thread.started", "control.response", "compaction.started", "compaction.failed"}
			wantCalls := int32(1)
			if trigger == domain.CompactionAuto {
				var err error
				cfg.ContextBudget, err = contextplan.NewBudget(22000, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				input = `{"version":1,"type":"input.submit","request_id":"request-1","text":"new user correction"}`
				want = []string{"thread.started", "control.response", "turn.started", "compaction.started", "compaction.failed", "assistant.text.delta", "turn.completed"}
				wantCalls++
			}
			seed, err := openChatResources(ctx, cfg, root, "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer seed.close(context.Background())
			submitAppTurn(t, seed, strings.Repeat("historical work ", 3500))
			thread := seed.identity.ThreadID
			if err := seed.close(ctx); err != nil {
				t.Fatal(err)
			}
			runner := &compactionFixtureRunner{t: t}
			hooks := compactionFixtureHooks(t, runner, string(trigger))
			hooks.interactiveApprovals = false
			resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, hooks)
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			output := &appWriteCloser{}
			outcome := runStreamJSON(ctx, Options{Input: io.NopCloser(strings.NewReader(input + "\n")), Output: output}, resources)
			if outcome.ExitCode() != 0 {
				t.Fatalf("stream failed: %#v, output=%s", outcome, output)
			}
			if err := resources.close(ctx); err != nil {
				t.Fatal(err)
			}
			assertPreCompactStreamFrames(t, output.Bytes(), want, trigger)
			if runner.runs != 0 || summaryCalls.Load() != 0 || samplingCalls.Load() != wantCalls {
				t.Fatalf("headless trust failure started a side effect: runner=%d summaries=%d requests=%d", runner.runs, summaryCalls.Load(), samplingCalls.Load())
			}
			loaded := loadHeadlessJournal(t, root, thread)
			for _, record := range loaded.Records {
				if record.EventKind == session.EventPreCompactHookStarted {
					t.Fatal("headless trust failure left a hook execution intent")
				}
			}
		})
	}
}

func assertPreCompactStreamFrames(t *testing.T, output []byte, kinds []string, trigger domain.CompactionTrigger) {
	t.Helper()
	type frame struct {
		Type         string                         `json:"type"`
		InputID      string                         `json:"input_id"`
		RequestID    string                         `json:"request_id"`
		CompactionID domain.CompactionID            `json:"compaction_id"`
		TurnID       domain.TurnID                  `json:"turn_id"`
		Scope        domain.CompactionScope         `json:"scope"`
		Code         protocol.CompactionFailureCode `json:"code"`
		Durable      bool                           `json:"durable"`
		UsageState   string                         `json:"usage_state"`
	}
	var turn domain.TurnID
	var admission, started domain.CompactionID
	lines := bytes.Split(bytes.TrimSpace(output), []byte("\n"))
	if len(lines) != len(kinds) {
		t.Fatalf("unexpected stream output: %s", output)
	}
	for index, line := range lines {
		var f frame
		if err := json.Unmarshal(line, &f); err != nil || f.Type != kinds[index] {
			t.Fatalf("unexpected frame %d: %s (want %s)", index, line, kinds[index])
		}
		switch f.Type {
		case "control.response":
			admission = f.CompactionID
		case "turn.started":
			turn = f.TurnID
			if !turn.Valid() || f.InputID != "request-1" {
				t.Fatalf("lost auto input identity: %s", line)
			}
		case "compaction.started", "compaction.failed":
			if f.Scope.Trigger != trigger || f.Scope.TurnID != turn || f.Scope.SampleIndex != 0 || f.RequestID != "request-1" ||
				!f.CompactionID.Valid() || trigger == domain.CompactionManual && f.CompactionID != admission {
				t.Fatalf("invalid compaction correlation: %s", line)
			}
			if f.Type == "compaction.started" {
				started = f.CompactionID
			} else if f.CompactionID != started || f.Code != protocol.CompactionFailureHookTrustRequired || !f.Durable || f.UsageState != "unavailable" {
				t.Fatalf("invalid matched failure: %s", line)
			}
		}
	}
}
