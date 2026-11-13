package app

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
)

func TestPluginWebSocketIncrementalPairingAndRestoredFullRequest(t *testing.T) {
	t.Setenv("HOME", privateAppTempDir(t))
	var mu sync.Mutex
	var requests [][]byte
	closed := make(chan struct{}, 2)
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
			mu.Lock()
			requests = append(requests, body)
			sample := len(requests)
			mu.Unlock()
			frames := pluginToolFrames(domain.ProviderOpenAI, sample, "claudeqa:review")
			if sample == 1 {
				reasoning := `data: {"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","id":"plugin-reason","summary":[],"encrypted_content":"opaque-plugin"}}` + "\n\n"
				frames = strings.Replace(frames, `data: {"type":"response.completed"`, reasoning+`data: {"type":"response.completed"`, 1)
			}
			for _, frame := range strings.Split(frames, "\n\n") {
				if frame != "" {
					if err := conn.Write(r.Context(), websocket.MessageText, []byte(strings.TrimPrefix(frame, "data: "))); err != nil {
						return
					}
				}
			}
		}
	}))
	defer server.Close()
	root, workspace := appPluginFixture(t, "claude"), t.TempDir()
	dataRoot := filepath.Join(privateAppTempDir(t), "sessions")
	cfg := cacheFixtureConfigMode(t, config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "gpt-test", BaseURL: server.URL, APIKey: secret.New("plugin-ws-fixture")}}, true, "websocket_auto")
	open := func(thread string) *chatResources {
		resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, thread, false, workspace, chatResourceHooks{pluginDirs: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		return resources
	}
	resources := open("")
	submitAppTurn(t, resources, "$claudeqa:review WS args")
	thread := resources.identity.ThreadID
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-closed
	restored := open(string(thread))
	submitAppTurn(t, restored, "followup")
	if err := restored.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-closed
	mu.Lock()
	captured := append([][]byte(nil), requests...)
	mu.Unlock()
	if len(captured) != 4 {
		t.Fatal("unexpected websocket sampling count", len(captured))
	}
	for i, call := range []string{"call-plugin-1", "call-plugin-2"} {
		var suffix struct {
			Previous string `json:"previous_response_id"`
			Input    []struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Output string `json:"output"`
			} `json:"input"`
		}
		if err := codec.Unmarshal(captured[i+1], &suffix); err != nil || suffix.Previous == "" || len(suffix.Input) != 1 || suffix.Input[0].Type != "function_call_output" || suffix.Input[0].CallID != call || suffix.Input[0].Output == "" {
			t.Fatalf("incorrect native incremental suffix: %v %s", err, captured[i+1])
		}
	}
	var full struct {
		Previous string `json:"previous_response_id"`
	}
	if codec.Unmarshal(captured[3], &full) != nil || full.Previous != "" || !bytes.Contains(captured[3], []byte("opaque-plugin")) || !bytes.Contains(captured[3], []byte("Review WS args")) || !bytes.Contains(captured[3], []byte("Check correctness")) {
		t.Fatal("restored full request lost frozen or opaque history")
	}
}
