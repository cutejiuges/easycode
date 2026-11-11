package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func TestReasoningAutomaticSummaryAfterToolAndManualRejection(t *testing.T) {
	clearProviderEnvironment(t)
	for _, tc := range []struct {
		family          domain.ProviderFamily
		manual, blocked bool
	}{
		{domain.ProviderOpenAI, false, false}, {domain.ProviderAnthropic, false, false},
		{domain.ProviderAnthropic, true, false}, {domain.ProviderAnthropic, true, true},
	} {
		t.Run(fmt.Sprintf("%s/manual=%t/blocked=%t", tc.family, tc.manual, tc.blocked), func(t *testing.T) {
			workspace := t.TempDir()
			content := "source material"
			if tc.blocked {
				content = strings.Repeat("large file line\n", 6000)
			}
			if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var bodies [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, body)
				n := len(bodies)
				mu.Unlock()
				summary := bytes.Contains(body, []byte(`"tool_choice"`))
				if tc.manual {
					if summary || !bytes.Contains(body, []byte(`"thinking":{"type":"enabled","budget_tokens":8192,"display":"omitted"}`)) || !bytes.Contains(body, []byte(`"max_tokens":16384`)) {
						t.Error("manual strategy changed or rejected summary reached network")
					}
				} else {
					assertReasoningRequest(t, tc.family, body, summary)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					_, _ = w.Write(activeReasoningFixture(tc.family, 1, true))
					return
				}
				if summary {
					if !bytes.Contains(body, []byte("opaque-reasoning-sentinel")) || (!bytes.Contains(body, []byte(`"type":"tool_result"`)) && !bytes.Contains(body, []byte(`"type":"function_call_output"`))) {
						t.Error("summary lost reasoning or durable tool pairing")
					}
					_, _ = w.Write(activeReasoningFixture(tc.family, 2, false))
					return
				}
				_, _ = w.Write(activeReasoningFixture(tc.family, n, false))
			}))
			defer server.Close()
			fields := reasoningFields(tc.family)
			if tc.manual {
				fields = `"anthropic_thinking":{"type":"enabled","budget_tokens":8192,"display":"omitted"},"anthropic_effort":"high","max_output_tokens":16384`
			}
			fields += `,"context_window_tokens":50000,"reserved_output_tokens":16384,"auto_compact":true`
			path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), tc.family, server.URL), fields)
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			resources, err := openChatResources(context.Background(), cfg, filepath.Join(t.TempDir(), "sessions"), "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			if err := resources.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			admitted, err := resources.session.Append(context.Background(), strings.Repeat("history ", 14000))
			if err != nil {
				t.Fatal(err)
			}
			if admitted.Status() != protocol.CommandStatusAccepted {
				t.Fatal("input rejected")
			}
			started, finished, failed := 0, 0, false
			var completedUsage domain.SampleUsage
			for item := range resources.session.Outputs() {
				event, ok := item.TurnEvent()
				if !ok {
					event, ok = item.ContextEvent()
				}
				if !ok {
					continue
				}
				switch event.Kind {
				case protocol.EventCompactionStarted:
					p, err := protocol.DecodeCompactionStarted(event)
					if err != nil || p.Scope.Trigger != domain.CompactionAuto || p.Scope.SampleIndex != 1 {
						t.Fatal("summary at wrong assistant boundary", p, err)
					}
					started++
				case protocol.EventCompactionCompleted:
					finished++
				case protocol.EventTurnCompleted:
					p, err := protocol.DecodeTurnCompleted(event)
					if err != nil {
						t.Fatal(err)
					}
					completedUsage, err = p.Usage.Domain()
					if err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnFailed:
					failed = true
				}
				if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed {
					break
				}
			}
			if failed != tc.blocked {
				t.Fatal("old window budget decision", failed, tc.blocked)
			}
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			captured := append([][]byte(nil), bodies...)
			mu.Unlock()
			want := 3
			if tc.manual {
				want = 2
			}
			if tc.blocked {
				want = 1
			}
			if len(captured) != want {
				t.Fatal("unexpected retry or summary admission", len(captured), want)
			}
			loaded := loadHeadlessJournal(t, resources.service.repository.RootPath(), resources.identity.ThreadID)
			plan, err := session.NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Tools) != 1 || !plan.Tools[0].HasResult {
				t.Fatal("tool repeated or result lost")
			}
			if tc.manual {
				if started != 0 || finished != 0 || len(plan.Compactions) != 0 {
					t.Fatal("preflight rejection wrote summary facts")
				}
			} else {
				if started != 1 || finished != 1 || len(plan.Compactions) != 1 || plan.Compactions[0].Completed == nil {
					t.Fatal("missing durable summary")
				}
				u, _ := plan.Compactions[0].Completed.Usage.Domain()
				v, k := u.ReasoningOutput().Value()
				if !k || v != 30 {
					t.Fatal("summary thinking lost")
				}
			}
			if len(plan.SampleUsages) < 1 {
				t.Fatal("prior sample usage erased by later failure")
			}
			for _, sample := range plan.SampleUsages {
				v, k := sample.Usage.ReasoningOutput().Value()
				if !k || v != 30 {
					t.Fatal("sample usage lost")
				}
			}
			if !tc.blocked {
				u := completedUsage
				v, _ := u.ReasoningOutput().Value()
				total, _ := u.Output().Value()
				if v != 60 || total != 80 {
					t.Fatal("summary counted in ordinary turn", v, total)
				}
			}
		})
	}
}
