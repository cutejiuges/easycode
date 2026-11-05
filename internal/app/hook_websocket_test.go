package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
	"github.com/coder/websocket"
)

func TestHookOpenAIIncrementalAndRestoredFullDeriveFromSameNativeHistory(t *testing.T) {
	var mu sync.Mutex
	var requests [][]byte
	connections := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		mu.Lock()
		connections++
		connection := connections
		mu.Unlock()
		for n := 1; ; n++ {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			mu.Lock()
			requests = append(requests, body)
			mu.Unlock()
			var frames bytes.Buffer
			if connection == 1 && n == 1 {
				writeExecCommandSample(&frames, domain.ProviderOpenAI, "printf never-run", 1000)
			} else {
				writeOpenAIAppTurn(&frames, 2)
			}
			for _, frame := range strings.Split(frames.String(), "\n\n") {
				if frame == "" {
					continue
				}
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(strings.TrimPrefix(frame, "data: "))); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	workspace := t.TempDir()
	writeHookConfig(t, workspace, "printf never-run-hook")
	cfg := cacheFixtureConfigMode(t, config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("synthetic-ws-key")}}, true, "websocket_auto")
	root := filepath.Join(t.TempDir(), "sessions")
	live, err := openChatResources(context.Background(), cfg, root, "", workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.close(context.Background())
	submitAppTurn(t, live, "seed")
	path, _ := live.service.repository.JournalPath(live.identity.ThreadID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(t.TempDir(), "sessions")
	repo, err := session.OpenOrCreateRepository(copied)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repo.Create(context.Background(), live.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
	copyPath, _ := repo.JournalPath(live.identity.ThreadID)
	if err := os.WriteFile(copyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	restored, err := openChatResources(context.Background(), cfg, copied, string(live.identity.ThreadID), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close(context.Background())
	submitAppTurn(t, live, "next")
	submitAppTurn(t, restored, "next")
	mu.Lock()
	captured := append([][]byte(nil), requests...)
	mu.Unlock()
	if len(captured) != 4 {
		t.Fatal("incorrect websocket request count", len(captured))
	}
	type request struct {
		Previous string            `json:"previous_response_id"`
		Input    []json.RawMessage `json:"input"`
		Tools    json.RawMessage   `json:"tools"`
	}
	var first, continuation, incremental, full request
	for i, p := range []*request{&first, &continuation, &incremental, &full} {
		if err := json.Unmarshal(captured[i], p); err != nil {
			t.Fatal(err)
		}
	}
	if continuation.Previous != "resp-exec" || !bytes.Contains(captured[1], []byte("hook_trust_required")) || incremental.Previous != "resp-2" || full.Previous != "" {
		t.Fatal("invalid full/incremental relationship")
	}
	if len(continuation.Input) != 1 || len(incremental.Input) != 1 || len(full.Input) != 5 {
		t.Fatal("incorrect ordered native suffix", len(continuation.Input), len(incremental.Input), len(full.Input))
	}
	for _, pair := range [][2]json.RawMessage{{first.Input[0], full.Input[0]}, {continuation.Input[0], full.Input[2]}, {incremental.Input[0], full.Input[4]}} {
		var a, b any
		if codec.Unmarshal(pair[0], &a) != nil || codec.Unmarshal(pair[1], &b) != nil || !reflect.DeepEqual(a, b) {
			t.Fatal("incremental suffix differs from restored full history")
		}
	}
	if !bytes.Equal(first.Tools, full.Tools) {
		t.Fatal("hooks changed stable tool definitions")
	}
	if _, err := os.Stat(filepath.Join(workspace, "never-run")); !os.IsNotExist(err) {
		t.Fatal("headless executed command")
	}
}
