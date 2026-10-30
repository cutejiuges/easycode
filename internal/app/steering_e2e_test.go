package app

import (
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSameTurnSteeringThroughBothProviderTransports(t *testing.T) {
	for _, tc := range []struct {
		family domain.ProviderFamily
		serve  func(io.Writer, int)
	}{{domain.ProviderOpenAI, writeOpenAIToolLoopSample}, {domain.ProviderAnthropic, writeAnthropicToolLoopSample}} {
		t.Run(string(tc.family), func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("content\n"), 0600); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
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
				index := len(requests)
				mu.Unlock()
				if index == 1 {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				tc.serve(w, index)
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: tc.family, BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "fixture-model"}}
			root := filepath.Join(t.TempDir(), "sessions")
			resources, err := openChatResources(context.Background(), cfg, root, "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			if err := resources.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			completed := make(chan []protocol.ControlItem, 1)
			go func() {
				var items []protocol.ControlItem
				for item := range resources.session.Outputs() {
					items = append(items, item)
					if event, ok := item.TurnEvent(); ok && (event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed) {
						break
					}
				}
				completed <- items
			}()
			if _, err := resources.session.Append(context.Background(), "Read README.md"); err != nil {
				t.Fatal(err)
			}
			<-entered
			second, err := resources.session.Append(context.Background(), "  preserve 中文\n")
			if err != nil {
				t.Fatal(err)
			}
			third, err := resources.session.Append(context.Background(), "also verify tests")
			if err != nil {
				t.Fatal(err)
			}
			once.Do(func() { close(release) })
			items := <-completed
			var turnID domain.TurnID
			committed := make(map[domain.InputID]int)
			starts := 0
			for _, item := range items {
				if event, ok := item.TurnEvent(); ok {
					if event.Kind == protocol.EventTurnFailed {
						t.Fatal("steering turn failed")
					}
					if event.Kind == protocol.EventTurnStarted {
						starts++
						turnID = event.TurnID
					}
					if event.Kind == protocol.EventUserInputCommitted {
						p, err := protocol.DecodeUserInputCommitted(event)
						if err != nil {
							t.Fatal(err)
						}
						committed[p.InputID]++
						if event.TurnID != turnID {
							t.Fatal("input moved to another turn")
						}
					}
				}
			}
			if starts != 1 || len(committed) != 3 || committed[second.EntryID()] != 1 || committed[third.EntryID()] != 1 {
				t.Fatal("input committed more than once or outside active turn")
			}
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 2 || strings.Contains(string(captured[0]), "preserve") || !strings.Contains(string(captured[1]), "preserve") || !strings.Contains(string(captured[1]), "also verify tests") {
				t.Fatal("safe point request omitted or duplicated steering")
			}
			threadID := resources.identity.ThreadID
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			restored, err := openChatResources(context.Background(), cfg, root, string(threadID), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.close(context.Background())
			history := restored.history
			if len(history.Turns) != 1 || history.Turns[0].TurnID != turnID {
				t.Fatal("restored turn identity changed")
			}
			var users []domain.SemanticMessage
			for _, m := range history.Turns[0].Messages {
				if m.Role == domain.SemanticUser {
					users = append(users, m)
				}
			}
			if len(users) != 3 || users[1].InputID != second.EntryID() || users[1].Text != "  preserve 中文\n" || users[2].InputID != third.EntryID() {
				t.Fatal("restored input identity or text changed")
			}
			mu.Lock()
			count := len(requests)
			mu.Unlock()
			if count != 2 {
				t.Fatal("restore automatically called provider")
			}
		})
	}
}
