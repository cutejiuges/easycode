package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"easycode/internal/config"
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
	writeAppTestFile(t, path, []byte(text), 0600)
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
	writeAppTestFile(t, path, data, 0600)
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
	return queryFixtureCacheSnapshot(t, ctx, resources, "completion-cache")
}

func queryFixtureCacheSnapshot(t *testing.T, ctx context.Context, resources *chatResources, requestID protocol.RequestID) protocol.CacheSnapshotPayload {
	t.Helper()
	if result, err := resources.session.QueryCache(ctx, requestID); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("cache snapshot not accepted", err)
	}
	for {
		select {
		case item, open := <-resources.session.Outputs():
			if !open {
				t.Fatal("session closed before cache snapshot")
			}
			if item.RequestID() != requestID {
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

// 代表场景复用完整矩阵的断言，覆盖两家 Provider、所有入口及真实恢复和关闭。
func TestCompletionAndPluginBrowserConcurrentLifecycle(t *testing.T) {
	t.Run("local", func(t *testing.T) { testCompletionRequestCases(t, false, "completion") })
	for _, selection := range []string{"completion", "plugins", "skills"} {
		t.Run(selection, func(t *testing.T) { testCompletionRequestCases(t, true, selection) })
	}
}

func testCompletionRequestCases(t *testing.T, pluginPackage bool, selection string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			const args = "\u0301中文 👩‍💻 args"
			const manualOnly = true
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
				writeAppTestFile(t, main, []byte(text), 0600)
				writeAppTestFile(t, filepath.Join(pack, "skills", "directory-name", "agents", "openai.yaml"), []byte(fmt.Sprintf("policy: {allow_implicit_invocation: %t}\n", !manualOnly)), 0600)
				pluginDirs = []string{pack}
			} else {
				writeCompletionSkill(t, workspace, manualOnly, "Frozen $ARGUMENTS")
			}
			var recorder requestRecorder
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, body := recorder.record(t, r)
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
			seedBytes := readAppTestFile(t, seedPath)
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
				before := readAppTestFile(t, path)
				beforeRequests := len(recorder.snapshot())
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
				after := readAppTestFile(t, path)
				afterRequests := len(recorder.snapshot())
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
			_, left, err := resources[0].transcriptQueries.projector.(provider.Conversation).ProjectContextWindow()
			if err != nil {
				t.Fatal(err)
			}
			_, right, err := resources[1].transcriptQueries.projector.(provider.Conversation).ProjectContextWindow()
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
			captured := recorder.snapshot()
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
