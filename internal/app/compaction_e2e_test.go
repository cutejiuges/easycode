package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func TestManualCompactionProvidersResumeEquivalent(t *testing.T) {
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
				compact := bytes.Contains(body, []byte(`"tool_choice"`))
				answer := 1
				if compact {
					answer = 2
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderOpenAI {
					writeOpenAIAppTurn(w, answer)
				} else {
					var data bytes.Buffer
					writeAnthropicAppTurn(&data, answer)
					wire := strings.Replace(data.String(), `data: {"type":"message_stop"}`, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}", 1)
					_, _ = io.WriteString(w, wire)
				}
			}))
			defer server.Close()
			budget, _ := contextplan.NewBudget(200000, 1000, 100)
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("compaction-secret"), Model: "claude-test"}, ContextBudget: budget}
			var nextRequests [][]byte
			for _, resume := range []bool{false, true} {
				root, cwd := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
				resources, err := openChatResources(context.Background(), cfg, root, "", cwd)
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.session.StartInteractive(context.Background()); err != nil {
					t.Fatal(err)
				}
				events, err := submitInteractiveFixture(t, resources.session, context.Background(), strings.Repeat("archived task details ", 1000))
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == protocol.EventTurnFailed {
						t.Fatalf("initial turn failed: %s", event.Payload)
					}
				}
				waitCompactionIdle(t, resources)
				var model tea.Model = tui.NewModel(context.Background(), "test", resources.session)
				model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/compact preserve unfinished tests")})
				var command tea.Cmd
				model, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if command == nil {
					t.Fatal("TUI did not dispatch compact")
				}
				model, _ = model.Update(command())
				var result protocol.CommandResult
				for item := range resources.session.Outputs() {
					model, _ = model.Update(item)
					if accepted, ok := item.CommandResult(); ok && accepted.CommandKind() == protocol.CommandCompactContext {
						result = accepted
						break
					}
				}
				if result.Status() != protocol.CommandStatusAccepted {
					t.Fatal("TUI compaction not accepted")
				}
				started, completed := 0, 0
				for item := range resources.session.Outputs() {
					model, _ = model.Update(item)
					event, ok := item.ContextEvent()
					if !ok {
						continue
					}
					if event.CompactionID != result.CompactionID() {
						t.Fatal("compaction correlation lost")
					}
					if event.Kind == protocol.EventCompactionStarted {
						started++
					}
					if event.Kind == protocol.EventCompactionFailed {
						t.Fatalf("compaction failed: %s", event.Payload)
					}
					if event.Kind == protocol.EventCompactionCompleted {
						completed++
						break
					}
				}
				if started != 1 || completed != 1 {
					t.Fatal("compaction lifecycle duplicated")
				}
				if !strings.Contains(model.View(), "Conversation compacted") {
					t.Fatal("TUI completion boundary missing")
				}
				waitCompactionIdle(t, resources)
				page, err := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: resources.identity.ThreadID, Limit: 50})
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != 3 || page.Items[0].Kind != domain.TranscriptCompaction || page.Items[1].Kind != domain.TranscriptAssistant || page.Items[2].Kind != domain.TranscriptUser {
					t.Fatalf("full transcript lost: %#v", page.Items)
				}
				body, err := resources.transcriptQueries.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: resources.identity.ThreadID, Cursor: page.Items[0].Cursor, Source: protocol.TranscriptText, Limit: 1024})
				if err != nil || body.Text != "answer-2" {
					t.Fatalf("summary detail=%q err=%v", body.Text, err)
				}
				if resume {
					thread := resources.identity.ThreadID
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					resources, err = openChatResources(context.Background(), cfg, root, string(thread), cwd)
					if err != nil {
						t.Fatal(err)
					}
					if len(resources.history.Turns) != 1 || !strings.Contains(resources.history.Turns[0].Messages[0].Text, "archived task details") {
						t.Fatal("resume lost original user input")
					}
					if err := resources.session.StartInteractive(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				events, err = submitInteractiveFixture(t, resources.session, context.Background(), "continue explicitly")
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == protocol.EventTurnFailed {
						t.Fatalf("continuation failed: %s", event.Payload)
					}
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				nextRequests = append(nextRequests, append([]byte{}, requests[len(requests)-1]...))
				mu.Unlock()
			}
			mu.Lock()
			count := len(requests)
			captured := append([][]byte{}, requests...)
			mu.Unlock()
			if count != 6 {
				t.Fatalf("unexpected automatic continuation or regeneration: %d requests", count)
			}
			if !bytes.Equal(nextRequests[0], nextRequests[1]) {
				t.Fatal("restored request differs from uninterrupted request")
			}
			if bytes.Contains(nextRequests[0], []byte("archived task details")) || bytes.Contains(nextRequests[0], []byte("preserve unfinished tests")) || !bytes.Contains(nextRequests[0], []byte("answer-2")) {
				t.Fatal("active window did not replace archived input")
			}
			for _, index := range []int{1, 4} {
				if !bytes.Contains(captured[index], []byte("preserve unfinished tests")) || !bytes.Contains(captured[index], []byte("archived task details")) {
					t.Fatal("summary request lost source or instructions")
				}
			}
		})
	}
}

// waitCompactionIdle 通过真实只读控制快照等待 owner 收到 worker 清理完成，不猜测调度时序。
func waitCompactionIdle(t *testing.T, resources *chatResources) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		if _, err := resources.session.QueryContext(ctx); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case item := <-resources.session.Outputs():
				event, ok := item.ContextEvent()
				if !ok || event.Kind != protocol.EventContextSnapshot {
					continue
				}
				snapshot, err := protocol.DecodeContextSnapshot(event)
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.Activity == "idle" {
					return
				}
			case <-ctx.Done():
				t.Fatal("owner did not finish cleanup")
			}
			break
		}
	}
}
