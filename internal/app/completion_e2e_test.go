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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func writeCompletionSkill(t *testing.T, workspace string, manual bool, content string) string {
	t.Helper()
	path := filepath.Join(workspace, ".easycode", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("---\ndescription: Review local changes\nargument-hint: '[args]'\ndisable-model-invocation: %t\n---\n%s", manual, content)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func copyCompletionJournal(t *testing.T, thread domain.ThreadID, data []byte) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sessions")
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Create(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := repo.JournalPath(thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

// runCompletionCommand 执行本次编辑返回的命令；长期控制流由测试显式有序消费。
func runCompletionCommand(t *testing.T, model *tea.Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	message := cmd()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, child := range batch {
			runCompletionCommand(t, model, child)
		}
	} else if message != nil {
		*model, _ = (*model).Update(message)
	}
}

func completionKey(t *testing.T, model *tea.Model, key tea.KeyMsg) {
	t.Helper()
	next, cmd := (*model).Update(key)
	*model = next
	runCompletionCommand(t, model, cmd)
}

func awaitCompletionTurn(t *testing.T, ctx context.Context, resources *chatResources, model *tea.Model) {
	t.Helper()
	commits := 0
	for {
		select {
		case item := <-resources.session.Outputs():
			*model, _ = (*model).Update(item)
			if event, ok := item.TurnEvent(); ok {
				if event.Kind == protocol.EventUserInputCommitted {
					commits++
				}
				if event.Kind == protocol.EventTurnFailed {
					t.Fatalf("completion turn failed: %s", event.Payload)
				}
				if event.Kind == protocol.EventTurnCompleted {
					if commits != 1 {
						t.Fatal("input was not committed exactly once", commits)
					}
					return
				}
			}
		case <-ctx.Done():
			t.Fatal("completion turn did not terminate", ctx.Err())
		}
	}
}

func completionCacheSnapshot(t *testing.T, ctx context.Context, resources *chatResources) protocol.CacheSnapshotPayload {
	t.Helper()
	if _, err := resources.session.QueryCache(ctx, "completion-cache"); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case item := <-resources.session.Outputs():
			if item.RequestID() != "completion-cache" {
				continue
			}
			if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventCacheSnapshot {
				p, err := protocol.DecodeCacheSnapshot(event)
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestCompletionManualAndRestoredRequestsBothProviders(t *testing.T) {
	testCompletionManualAndRestoredRequests(t, false, "completion")
}

func TestPluginBrowserManualAndRestoredRequestsBothProviders(t *testing.T) {
	for _, selection := range []string{"completion", "plugins", "skills"} {
		t.Run(selection, func(t *testing.T) { testCompletionManualAndRestoredRequests(t, true, selection) })
	}
}

func testCompletionManualAndRestoredRequests(t *testing.T, pluginPackage bool, selection string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, manualOnly := range []bool{false, true} {
			for _, inputCase := range []struct{ name, args string }{{"ascii", "exact args"}, {"unicode", "\u0301中文 👩‍💻 args"}} {
				t.Run(fmt.Sprintf("%s/manual-only-%t/%s", family, manualOnly, inputCase.name), func(t *testing.T) {
					args := inputCase.args
					name, sigil := "review", "$"
					var pluginDirs []string
					if pluginPackage {
						name = "codexqa:review"
						if selection != "completion" {
							sigil = "/"
						}
					}
					inputText := sigil + name + " " + args
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					workspace := t.TempDir()
					if pluginPackage {
						pack := appPluginFixture(t, "codex")
						main := filepath.Join(pack, "skills", "directory-name", "SKILL.md")
						text := fmt.Sprintf("---\nname: review\ndescription: Review local changes\nargument-hint: '[args]'\ndisable-model-invocation: %t\n---\nFrozen $ARGUMENTS", manualOnly)
						if err := os.WriteFile(main, []byte(text), 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(pack, "skills", "directory-name", "agents", "openai.yaml"), []byte(fmt.Sprintf("policy: {allow_implicit_invocation: %t}\n", !manualOnly)), 0600); err != nil {
							t.Fatal(err)
						}
						pluginDirs = []string{pack}
					} else {
						writeCompletionSkill(t, workspace, manualOnly, "Frozen $ARGUMENTS")
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
						mu.Unlock()
						n := 1
						if bytes.Contains(body, []byte(sigil+name)) {
							n = 2
						}
						if bytes.Contains(body, []byte("follow-up")) {
							n = 3
						}
						w.Header().Set("Content-Type", "text/event-stream")
						if family == domain.ProviderOpenAI {
							writeOpenAIAppTurn(w, n)
						} else {
							writeAnthropicAppTurn(w, n)
						}
					}))
					defer server.Close()
					cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("completion-fixture"), Model: "fixture-model"}}, true)
					open := func(root, thread string) (*chatResources, error) {
						return openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, thread, false, workspace, chatResourceHooks{pluginDirs: pluginDirs})
					}
					seed, err := open(filepath.Join(t.TempDir(), "sessions"), "")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { seed.close(context.Background()) })
					// 普通启动同样由真实装配提供冻结目录。
					seedModel, err := assembleChatModel(ctx, seed)
					if err != nil {
						t.Fatal(err)
					}
					var model tea.Model = seedModel
					completionKey(t, &model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$")})
					if !strings.Contains(model.View(), "$"+name) {
						t.Fatal("fresh assembly omitted explicit Skill")
					}
					completionKey(t, &model, tea.KeyMsg{Type: tea.KeyBackspace})
					completionKey(t, &model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("seed"), Paste: true})
					completionKey(t, &model, tea.KeyMsg{Type: tea.KeyEnter})
					awaitCompletionTurn(t, ctx, seed, &model)
					thread := seed.identity.ThreadID
					seedPath, _ := seed.service.repository.JournalPath(thread)
					seedBytes, err := os.ReadFile(seedPath)
					if err != nil {
						t.Fatal(err)
					}
					if err := seed.close(ctx); err != nil {
						t.Fatal(err)
					}
					roots := []string{copyCompletionJournal(t, thread, seedBytes), copyCompletionJournal(t, thread, seedBytes)}
					var resources [2]*chatResources
					var models [2]tea.Model
					var snapshots [2]protocol.CacheSnapshotPayload
					for branch := 0; branch < 2; branch++ {
						res, err := open(roots[branch], string(thread))
						if err != nil {
							t.Fatal(err)
						}
						resources[branch] = res
						t.Cleanup(func() { res.close(context.Background()) })
						assembled, err := assembleChatModel(ctx, res)
						if err != nil {
							t.Fatal(err)
						}
						models[branch] = assembled
						path, _ := res.service.repository.JournalPath(thread)
						before, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						mu.Lock()
						beforeRequests := len(requests)
						mu.Unlock()
						if branch == 0 {
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(inputText), Paste: true})
						} else if pluginPackage && selection != "completion" {
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/" + selection)})
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyEnter})
							if selection == "plugins" {
								completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyEnter})
							}
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyEnter})
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(args), Paste: true})
						} else {
							prefix := "$rev"
							if pluginPackage {
								prefix = "$codexqa:rev"
							}
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(prefix + " " + args)})
							completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyCtrlA})
							for i := 0; i < len(prefix); i++ {
								completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyRight})
							}
							for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyTab}, {Type: tea.KeyTab}} {
								completionKey(t, &models[branch], key)
							}
						}
						after, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						mu.Lock()
						afterRequests := len(requests)
						mu.Unlock()
						if !bytes.Equal(before, after) || beforeRequests != afterRequests {
							t.Fatal("completion produced execution or durable facts")
						}
						completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyEnter})
						awaitCompletionTurn(t, ctx, res, &models[branch])
						history := res.transcriptQueries.projector.(provider.Conversation).ProjectHistory()
						count := 0
						for _, turn := range history.Turns {
							for _, message := range turn.Messages {
								if message.HasSkill {
									count++
									if message.Text != inputText || message.Skill.Arguments != args || strings.Count(message.Skill.Content, "Frozen "+args) != 1 {
										t.Fatal("input or attachment changed")
									}
								}
							}
						}
						if count != 1 {
							t.Fatal("Skill was not prepared and committed exactly once", count)
						}
						snapshots[branch] = completionCacheSnapshot(t, ctx, res)
					}
					if snapshots[0].StableFingerprint != snapshots[1].StableFingerprint || snapshots[0].ContentFingerprint != snapshots[1].ContentFingerprint || !reflect.DeepEqual(snapshots[0].Sources, snapshots[1].Sources) || snapshots[0].LastSample.NativeFingerprint != snapshots[1].LastSample.NativeFingerprint {
						t.Fatal("completion changed cache/native fingerprints")
					}
					left, err := resources[0].transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
					if err != nil {
						t.Fatal(err)
					}
					right, err := resources[1].transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
					if err != nil || !reflect.DeepEqual(left, right) {
						t.Fatal("native footprint differs", err)
					}
					if err := resources[1].close(ctx); err != nil {
						t.Fatal(err)
					}
					resumed, err := open(roots[1], string(thread))
					if err != nil {
						t.Fatal(err)
					}
					resources[1] = resumed
					t.Cleanup(func() { resumed.close(context.Background()) })
					assembled, err := assembleChatModel(ctx, resumed)
					if err != nil {
						t.Fatal(err)
					}
					models[1] = assembled
					for branch := 0; branch < 2; branch++ {
						completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("follow-up"), Paste: true})
						completionKey(t, &models[branch], tea.KeyMsg{Type: tea.KeyEnter})
						awaitCompletionTurn(t, ctx, resources[branch], &models[branch])
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if len(captured) != 5 || !bytes.Equal(captured[1], captured[2]) || !bytes.Equal(captured[3], captured[4]) {
						t.Fatal("manual/completed/restored canonical request bytes differ", len(captured))
					}
					assertRequestFingerprintEqual(t, captured[1], captured[2])
					assertRequestFingerprintEqual(t, captured[3], captured[4])
					firstAnswer, secondAnswer := bytes.Index(captured[3], []byte("answer-1")), bytes.Index(captured[3], []byte("answer-2"))
					if firstAnswer < 0 || secondAnswer < 0 || firstAnswer > secondAnswer {
						t.Fatal("native item order changed")
					}
					for _, body := range captured {
						if bytes.Contains(body, []byte("completion-fixture")) || bytes.Contains(body, []byte(workspace)) || bytes.Contains(body, []byte("suppressedToken")) {
							t.Fatal("UI state or secret leaked")
						}
					}
				})
			}
		}
	}
}

func TestCompletionPreparationFailuresRemainEditable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, failure := range []string{"source-changed", "cancelled", "budget"} {
			t.Run(string(family)+"/"+failure, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				workspace := t.TempDir()
				content := "Frozen $ARGUMENTS"
				if failure == "budget" {
					content = strings.Repeat("content ", 7000)
				}
				path := writeCompletionSkill(t, workspace, false, content)
				server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("failed preparation contacted Provider") }))
				defer server.Close()
				cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "fixture-model"}}
				if failure == "budget" {
					var err error
					cfg.ContextBudget, err = contextplan.NewBudget(4096, 512, 128)
					if err != nil {
						t.Fatal(err)
					}
				}
				res, err := openChatResources(ctx, cfg, filepath.Join(t.TempDir(), "sessions"), "", workspace)
				if err != nil {
					t.Fatal(err)
				}
				defer res.close(context.Background())
				assembled, err := assembleChatModel(ctx, res)
				if err != nil {
					t.Fatal(err)
				}
				var model tea.Model = assembled
				journal, _ := res.service.repository.JournalPath(res.identity.ThreadID)
				before, err := os.ReadFile(journal)
				if err != nil {
					t.Fatal(err)
				}
				if failure == "source-changed" {
					if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				completionKey(t, &model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$rev")})
				completionKey(t, &model, tea.KeyMsg{Type: tea.KeyEnter})
				if !strings.Contains(model.View(), "$review") || strings.Contains(model.View(), "Error:") {
					t.Fatal("completion read Skill before submission")
				}
				if failure == "cancelled" {
					cancel()
				}
				completionKey(t, &model, tea.KeyMsg{Type: tea.KeyEnter})
				if failure == "budget" {
					deadline, stop := context.WithTimeout(context.Background(), 5*time.Second)
					defer stop()
					for {
						select {
						case item := <-res.session.Outputs():
							model, _ = model.Update(item)
							if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventTurnFailed {
								goto failed
							}
						case <-deadline.Done():
							t.Fatal("budget rejection did not settle")
						}
					}
				}
			failed:
				after, err := os.ReadFile(journal)
				if err != nil {
					t.Fatal(err)
				}
				// budget 拒绝可记录 turn 生命周期，但不能保存被拒绝的用户输入或 Skill 正文。
				if failure != "budget" && !bytes.Equal(before, after) {
					t.Fatal("failed preparation wrote journal")
				}
				if bytes.Contains(after, []byte("Frozen")) || bytes.Contains(after, []byte("$review")) {
					t.Fatal("rejected input persisted")
				}
				if !strings.Contains(model.View(), "$review") {
					t.Fatal("failed preparation lost editable draft")
				}
			})
		}
	}
}
