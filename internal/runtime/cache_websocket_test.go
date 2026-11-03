package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
	"easycode/internal/session"
	"github.com/coder/websocket"
)

func TestWebSocketDurabilityAndOuterCancellation(t *testing.T) {
	for _, mode := range []string{"success", "sync_failure", "outer_cancel"} {
		t.Run(mode, func(t *testing.T) {
			bodies := make(chan []byte, 4)
			closed := make(chan struct{}, 4)
			var sequence atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				defer func() { closed <- struct{}{} }()
				for {
					_, body, err := conn.Read(r.Context())
					if err != nil {
						return
					}
					bodies <- body
					id := fmt.Sprintf("r%d", sequence.Add(1))
					for _, event := range []string{fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id), `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`, fmt.Sprintf(`{"type":"response.completed","response":{"id":%q}}`, id)} {
						if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
							return
						}
					}
				}
			}))
			defer server.Close()
			policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIWebSocketAuto)
			p, err := openai.New(openai.Config{BaseURL: server.URL, APIKey: secret.New("private-key"), Model: "gpt-test", CachePolicy: policy})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			conversation, err := p.NewConversation(runtimeThreadID)
			if err != nil {
				t.Fatal(err)
			}
			journal := &fakeJournal{}
			if mode == "sync_failure" {
				journal.failAt = 3
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			config := testRuntimeConfig(t, journal)
			config.CachePolicy = policy
			config.GenerateTurnID = sequentialTurnIDs()
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			journal.hook = func(drafts []session.RecordDraft) {
				for _, draft := range drafts {
					if draft.EventKind() == session.EventSampleUsage {
						if runtime.cache.snapshot.LastSample != nil {
							t.Error("baseline published before Sync returned")
						}
						if mode == "outer_cancel" {
							cancel()
						}
					}
				}
			}
			_, err = collectTurn(runtime, ctx, nil)
			<-bodies
			if mode == "sync_failure" {
				if err == nil || runtime.cache.snapshot.LastSample != nil {
					t.Fatal("Sync failure published observation")
				}
				<-closed
				return
			}
			if err != nil || runtime.cache.snapshot.LastSample == nil {
				t.Fatal("durable sample missing", err)
			}
			journal.hook = nil
			user, err := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "next")
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.RunTurn(context.Background(), []domain.UserInput{user}, func(protocol.Event) {}); err != nil {
				t.Fatal(err)
			}
			var next struct {
				Previous string `json:"previous_response_id"`
			}
			if err := codec.Unmarshal(<-bodies, &next); err != nil {
				t.Fatal(err)
			}
			if (next.Previous != "") != (mode == "success") {
				t.Fatalf("wrong continuation after %s: %q", mode, next.Previous)
			}
			_ = p.Close()
		})
	}
}
