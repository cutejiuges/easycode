package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"github.com/coder/websocket"
)

func TestWebSocketToolsAndCacheQueryEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var requests [][]byte
	closed := make(chan struct{})
	firstEntered, releaseFirst := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		defer close(closed)
		for n := 1; ; n++ {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			mu.Lock()
			requests = append(requests, body)
			mu.Unlock()
			if n == 1 {
				close(firstEntered)
				<-releaseFirst
			}
			var frames bytes.Buffer
			writeOpenAIParallelSearchSample(&frames, n)
			for _, frame := range strings.Split(frames.String(), "\n\n") {
				if frame == "" {
					continue
				}
				data := strings.TrimPrefix(frame, "data: ")
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(data)); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src/main.go"), []byte("package main\n// needle\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := cacheFixtureConfigMode(t, config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "gpt-test", BaseURL: server.URL, APIKey: secret.New("ws-secret")}}, true, "websocket_auto")
	resources, err := openChatResources(context.Background(), cfg, filepath.Join(t.TempDir(), "sessions"), "", workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	if err := resources.session.StartInteractive(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := submitInteractiveFixture(t, resources.session, context.Background(), "Find and inspect the matching source")
	if err != nil {
		t.Fatal(err)
	}
	<-firstEntered
	if result, err := resources.session.Append(context.Background(), "new exact correction"); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal(result, err)
	}
	close(releaseFirst)
	terminals := 0
	for e := range events {
		if e.Kind == protocol.EventTurnFailed {
			t.Fatalf("tool turn failed: %s", e.Payload)
		}
		if e.Kind == protocol.EventTurnCompleted {
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatal("missing unique terminal")
	}
	command, err := resources.session.QueryCache(context.Background(), "cache-e2e")
	if err != nil || command.Disposition() != protocol.CommandDispositionQueried {
		t.Fatal(command, err)
	}
	var snapshot protocol.CacheSnapshotPayload
	responseSeen := false
	for item := range resources.session.Outputs() {
		if item.RequestID() != "cache-e2e" {
			continue
		}
		if _, ok := item.CommandResult(); ok {
			responseSeen = true
			continue
		}
		if e, ok := item.ContextEvent(); ok {
			if !responseSeen {
				t.Fatal("snapshot preceded response")
			}
			snapshot, err = protocol.DecodeCacheSnapshot(e)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if snapshot.LastSample == nil || snapshot.LastSample.Mode != domain.RequestIncremental || snapshot.LastSample.Transport != domain.TransportWebSocket || snapshot.LastTurn == nil || snapshot.LastSummary != nil {
		t.Fatal("incorrect tool loop cache observations")
	}
	mu.Lock()
	captured := append([][]byte(nil), requests...)
	mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("tools repeated requests: %d", len(captured))
	}
	var second struct {
		Type     string            `json:"type"`
		Previous string            `json:"previous_response_id"`
		Input    []json.RawMessage `json:"input"`
		Store    bool              `json:"store"`
	}
	if err := codec.Unmarshal(captured[1], &second); err != nil {
		t.Fatal(err)
	}
	if second.Previous != "resp-search" || len(second.Input) != 4 || second.Store {
		t.Fatalf("bad ordered suffix: %s", captured[1])
	}
	for i, id := range []string{"call-glob", "call-grep", "call-read"} {
		var item struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}
		if codec.Unmarshal(second.Input[i], &item) != nil || item.Type != "function_call_output" || item.CallID != id || item.Output == "" {
			t.Fatal("native pairing or frozen result lost")
		}
	}
	if !bytes.Contains(second.Input[3], []byte("new exact correction")) {
		t.Fatal("durable steering omitted from ordered incremental suffix")
	}
	path, _ := resources.service.repository.JournalPath(resources.identity.ThreadID)
	journal, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ws-secret", "cache_snapshot", "cache.query", "prompt_cache_key", "previous_response_id"} {
		if bytes.Contains(journal, []byte(forbidden)) {
			t.Fatal("transient cache state entered journal", forbidden)
		}
	}
	repoRoot := resources.service.repository.RootPath()
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-closed
	// 连接 owner 完成后才释放 writer lease，原事实可以立即重新打开。
	repo, err := session.OpenOrCreateRepository(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Open(context.Background(), resources.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	_ = lease.Close()
}
