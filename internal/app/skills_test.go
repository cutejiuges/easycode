package app

import (
	"bytes"
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalSkillToolAndExplicitInputRestoreBothProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace, dataRoot := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
			p := filepath.Join(workspace, ".easycode", "skills", "demo", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			writeAppTestFile(t, p, []byte("---\ndescription: Review local changes\n---\nFrozen instructions $ARGUMENTS"), 0600)
			var requests requestRecorder
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index, _ := requests.record(t, r)
				n := index + 1
				var frames bytes.Buffer
				if family == domain.ProviderOpenAI {
					writeOpenAIToolLoopSample(&frames, n)
				} else {
					writeAnthropicToolLoopSample(&frames, n)
				}
				text := strings.NewReplacer(`"name":"Read"`, `"name":"Skill"`, "file_path", "skill", "README.md", "demo").Replace(frames.String())
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, text)
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("skill-test-key")}}
			resources := mustOpenChatResources(t, context.Background(), cfg, dataRoot, "", workspace)
			submitAppTurn(t, resources, "$demo exact args")
			thread := resources.identity.ThreadID
			journalPath, pathErr := resources.service.repository.JournalPath(thread)
			if pathErr != nil {
				t.Fatal(pathErr)
			}
			closeAppTestResources(t, resources)
			captured := requests.snapshot()
			if len(captured) != 2 {
				t.Fatalf("requests %d", len(captured))
			}
			if !bytes.Contains(captured[0], []byte("Frozen instructions exact args")) || !bytes.Contains(captured[0], []byte("$demo exact args")) || !bytes.Contains(captured[1], []byte("Frozen instructions")) {
				t.Fatal("missing explicit or tool content")
			}
			for _, r := range captured {
				if bytes.Contains(r, []byte(workspace)) || bytes.Contains(r, []byte("skill-test-key")) {
					t.Fatal("source or secret leaked")
				}
			}
			baseline := readAppTestFile(t, journalPath)
			loaded, plan := loadToolLoopPlan(t, dataRoot, thread)
			var ready, result bool
			for _, record := range loaded.Records {
				switch record.EventKind {
				case session.EventToolCallReady:
					v, e := session.DecodeToolCallReadyPayload(record)
					if e != nil {
						t.Fatal(e)
					}
					ready = v.Capability == tool.CapabilitySkill
				case session.EventToolCallResult:
					v, e := session.DecodeToolCallResultPayload(record)
					if e != nil {
						t.Fatal(e)
					}
					result = v.Capability == tool.CapabilitySkill && v.Status == tool.ResultSuccess
				}
			}
			if !ready || !result {
				t.Fatalf("Skill not durably completed: ready=%v result=%v plan=%v", ready, result, plan)
			}
			if err := os.RemoveAll(filepath.Join(workspace, ".easycode")); err != nil {
				t.Fatal(err)
			}
			restored := mustOpenChatResources(t, context.Background(), cfg, dataRoot, string(thread), workspace)
			defer restored.close(context.Background())
			m := restored.history.Turns[0].Messages[0]
			if m.Text != "$demo exact args" || !m.HasSkill || !strings.Contains(m.Skill.Content, "exact args") {
				t.Fatal("restored attachment reread or lost")
			}
			closeAppTestResources(t, restored)
			for _, record := range loaded.Records {
				if record.EventKind != session.EventToolCallReady && record.EventKind != session.EventToolExecutionStarted && record.EventKind != session.EventToolCallResult {
					continue
				}
				t.Run(string(record.EventKind), func(t *testing.T) {
					writeJournalPrefix(t, journalPath, baseline, record.Sequence)
					before := len(requests.snapshot())
					resumed := mustOpenChatResources(t, context.Background(), cfg, dataRoot, string(thread), workspace)
					closeAppTestResources(t, resumed)
					after := len(requests.snapshot())
					if before != after {
						t.Fatal("recovery contacted Provider")
					}
					facts, _ := loadToolLoopPlan(t, dataRoot, thread)
					found := false
					for _, fact := range facts.Records {
						if fact.EventKind != session.EventToolCallResult {
							continue
						}
						p, err := session.DecodeToolCallResultPayload(fact)
						if err != nil {
							t.Fatal(err)
						}
						found = true
						switch record.EventKind {
						case session.EventToolCallReady:
							if p.Status != tool.ResultCancelled {
								t.Fatal("ready call re-executed")
							}
						case session.EventToolExecutionStarted:
							if p.Status != tool.ResultOutcomeUncertain {
								t.Fatal("started call re-executed")
							}
						case session.EventToolCallResult:
							if p.Status != tool.ResultSuccess || !strings.Contains(p.Preview, "Frozen instructions") {
								t.Fatal("result/output recovery lost frozen instructions")
							}
						}
					}
					if !found {
						t.Fatal("missing reconciled Skill result")
					}
				})
			}
		})
	}
}
