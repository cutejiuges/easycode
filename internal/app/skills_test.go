package app

import (
	"bytes"
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalSkillToolAndExplicitInputRestoreBothProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, mode := range []string{"once", "interactive", "stream"} {
		for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
			t.Run(mode+"/"+string(family), func(t *testing.T) {
				workspace, dataRoot := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
				p := filepath.Join(workspace, ".easycode", "skills", "demo", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("---\ndescription: Review local changes\n---\nFrozen instructions $ARGUMENTS"), 0600); err != nil {
					t.Fatal(err)
				}
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					requests = append(requests, body)
					n := len(requests)
					mu.Unlock()
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
				resources, err := openChatResources(context.Background(), cfg, dataRoot, "", workspace)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "once":
					submitAppTurn(t, resources, "$demo exact args")
				case "interactive":
					if err := resources.session.StartInteractive(context.Background()); err != nil {
						t.Fatal(err)
					}
					events, err := submitInteractiveFixture(t, resources.session, context.Background(), "$demo exact args")
					if err != nil {
						t.Fatal(err)
					}
					completed := false
					for event := range events {
						if event.Kind == protocol.EventTurnFailed {
							t.Fatal("interactive skill turn failed")
						}
						completed = completed || event.Kind == protocol.EventTurnCompleted
					}
					if !completed {
						t.Fatal("interactive skill not completed")
					}
				case "stream":
					output := &appWriteCloser{}
					outcome := runStreamJSON(context.Background(), Options{Input: io.NopCloser(strings.NewReader(`{"version":1,"type":"input.submit","request_id":"skill","text":"$demo exact args"}` + "\n")), Output: output}, resources)
					if outcome.Class != ExitSuccess {
						t.Fatalf("stream skill: %#v %s", outcome, output.String())
					}
				}
				thread := resources.identity.ThreadID
				journalPath, pathErr := resources.service.repository.JournalPath(thread)
				if pathErr != nil {
					t.Fatal(pathErr)
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
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
				baseline, readErr := os.ReadFile(journalPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
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
				restored, err := openChatResources(context.Background(), cfg, dataRoot, string(thread), workspace)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.close(context.Background())
				m := restored.history.Turns[0].Messages[0]
				if m.Text != "$demo exact args" || !m.HasSkill || !strings.Contains(m.Skill.Content, "exact args") {
					t.Fatal("restored attachment reread or lost")
				}
				if err := restored.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				if mode == "once" {
					for _, record := range loaded.Records {
						if record.EventKind != session.EventToolCallReady && record.EventKind != session.EventToolExecutionStarted && record.EventKind != session.EventToolCallResult {
							continue
						}
						t.Run(string(record.EventKind), func(t *testing.T) {
							writeJournalPrefix(t, journalPath, baseline, record.Sequence)
							mu.Lock()
							before := len(requests)
							mu.Unlock()
							resumed, err := openChatResources(context.Background(), cfg, dataRoot, string(thread), workspace)
							if err != nil {
								t.Fatal(err)
							}
							if err := resumed.close(context.Background()); err != nil {
								t.Fatal(err)
							}
							mu.Lock()
							after := len(requests)
							mu.Unlock()
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
				}

			})
		}
	}
}

func TestSkillPreparationFailureLeavesJournalAndProviderUntouched(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("failed preparation contacted Provider") }))
	defer server.Close()
	resources, err := openChatResources(context.Background(), config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "test", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}, filepath.Join(t.TempDir(), "sessions"), "", workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	path, err := resources.service.repository.JournalPath(resources.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := resources.session.Submit(context.Background(), "$missing args"); err == nil || !strings.Contains(err.Error(), "skill_not_found") {
		t.Fatalf("missing safe skill classification: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resources.session.Submit(ctx, "$missing"); err == nil {
		t.Fatal("cancelled preparation accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("preparation failure wrote input")
	}
}
