package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/secret"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// composerWireUsers 只读取原生用户文本，不重新构造 Provider 请求。
func composerWireUsers(t *testing.T, body []byte) []string {
	t.Helper()
	type message struct {
		Role    string `json:"role"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	var wire struct {
		Input    []message `json:"input"`
		Messages []message `json:"messages"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, m := range append(wire.Input, wire.Messages...) {
		if m.Role == "user" {
			var text string
			for _, p := range m.Content {
				text += p.Text
			}
			users = append(users, text)
		}
	}
	return users
}

func TestComposerProvidersPersistExactTextAcrossResizeAndResume(t *testing.T) {
	const prompt = "  中文\n\tcode  \n"
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
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
				w.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderOpenAI {
					writeOpenAIAppTurn(w, 1)
				} else {
					writeAnthropicAppTurn(w, 1)
				}
			}))
			defer server.Close()
			budget, err := contextplan.NewBudget(200000, 20000, 4096)
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "test-model"}, ContextBudget: budget}
			for _, width := range []int{80, 32} {
				root, cwd := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
				resources, err := openChatResources(context.Background(), cfg, root, "", cwd)
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.session.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				var model tea.Model = tui.NewModel(context.Background(), "test", resources.session, resources.history)
				model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: 10})
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  中文\r\n\tcode  \r\n"), Paste: true})
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				for cmd != nil {
					model, cmd = model.Update(cmd())
				}
				if !strings.Contains(model.View(), "answer-1") {
					t.Fatalf("turn failed: %s", model.View())
				}
				id := resources.identity.ThreadID
				if width == 32 {
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					resources, err = openChatResources(context.Background(), cfg, root, string(id), cwd)
					if err != nil {
						t.Fatal(err)
					}
					if len(resources.history.Turns) != 1 || resources.history.Turns[0].UserText != prompt {
						t.Fatalf("history=%#v", resources.history)
					}
				}
				submitAppTurn(t, resources, " next\n")
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				// 已停止的真实会话拒绝 admission；重复 Enter 不产生请求或幽灵消息。
				var rejected tea.Model = tui.NewModel(context.Background(), "test", resources.session)
				rejected, _ = rejected.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(prompt), Paste: true})
				rejected, cmd = rejected.Update(tea.KeyMsg{Type: tea.KeyEnter})
				var duplicate tea.Cmd
				rejected, duplicate = rejected.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if duplicate != nil {
					t.Fatal("duplicate pending admission")
				}
				rejected, _ = rejected.Update(cmd())
				if strings.Contains(rejected.View(), "User:") || !strings.Contains(rejected.View(), "中文") {
					t.Fatal("rejection lost draft or added transcript")
				}
			}
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 4 {
				t.Fatalf("requests=%d", len(captured))
			}
			for _, i := range []int{0, 2} {
				users := composerWireUsers(t, captured[i])
				if len(users) != 1 || users[0] != prompt {
					t.Fatalf("users=%q", users)
				}
			}
			if string(captured[0]) != string(captured[2]) || string(captured[1]) != string(captured[3]) {
				t.Fatal("resize or restore changed canonical request")
			}
		})
	}
}

func TestComposerHeadlessPreservesWhitespaceForBothProviders(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			for _, prompt := range []string{"  中文\r\n\tcode  \n", " \t\r\n"} {
				t.Run(fmt.Sprintf("%s/%d/%q", family, mode, prompt), func(t *testing.T) {
					t.Chdir(t.TempDir())
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
						w.Header().Set("Content-Type", "text/event-stream")
						if family == domain.ProviderOpenAI {
							writeOpenAIAppTurn(w, 1)
						} else {
							writeAnthropicAppTurn(w, 1)
						}
					}))
					defer server.Close()
					output := &appWriteCloser{}
					options := Options{Mode: mode, ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"), Output: output}
					if mode == headless.ModeStreamJSON {
						encoded, err := json.Marshal(prompt)
						if err != nil {
							t.Fatal(err)
						}
						options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"composer\",\"text\":" + string(encoded) + "}\n"))
					} else {
						options.Prompt = prompt
					}
					outcome := Run(context.Background(), options)
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if strings.TrimSpace(prompt) == "" {
						if len(captured) != 0 {
							t.Fatal("blank input reached provider")
						}
						return
					}
					if outcome.ExitCode() != 0 || len(captured) != 1 {
						t.Fatalf("outcome=%#v requests=%d output=%s", outcome, len(captured), output.String())
					}
					users := composerWireUsers(t, captured[0])
					if len(users) != 1 || users[0] != prompt {
						t.Fatalf("users=%q", users)
					}
				})
			}
		}
	}
}
