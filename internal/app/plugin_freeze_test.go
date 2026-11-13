package app

import (
	"bytes"
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestQueuedPluginAttachmentDoesNotRereadDeletedSource(t *testing.T) {
	t.Setenv("HOME", privateAppTempDir(t))
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				requests = append(requests, body)
				sample := len(requests)
				mu.Unlock()
				if sample == 1 {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderOpenAI {
					writeOpenAIAppTurn(w, sample)
				} else {
					writeAnthropicAppTurn(w, sample)
				}
			}))
			defer server.Close()
			root := appPluginFixture(t, "claude")
			cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture")}}
			resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: filepath.Join(privateAppTempDir(t), "sessions")}, "", false, t.TempDir(), chatResourceHooks{pluginDirs: []string{root}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := resources.session.StartInteractive(ctx); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			collectorDone := make(chan struct{})
			go func() {
				defer close(collectorDone)
				terminals := 0
				for item := range resources.session.Outputs() {
					if event, ok := item.TurnEvent(); ok {
						if event.Kind == protocol.EventTurnFailed {
							finished <- fmt.Errorf("queued plugin turn failed")
							return
						}
						if event.Kind == protocol.EventTurnCompleted {
							terminals++
							if terminals == 2 {
								finished <- nil
								return
							}
						}
					}
				}
				finished <- fmt.Errorf("queued plugin stream ended early")
			}()
			defer func() { cancel(); _ = resources.close(context.Background()); <-collectorDone }()
			if _, err := resources.session.Append(ctx, "seed"); err != nil {
				t.Fatal(err)
			}
			<-entered
			result, err := resources.session.Append(ctx, "$claudeqa:review frozen args")
			if err != nil || result.Status() != protocol.CommandStatusAccepted {
				t.Fatal("queue not accepted", err)
			}
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 2 || !bytes.Contains(captured[1], []byte("Review frozen args")) {
				t.Fatal("queue reread or lost prepared instructions")
			}
		})
	}
}

func TestChangedSidecarOnlyChangesFutureCatalogOnRestore(t *testing.T) {
	t.Setenv("HOME", privateAppTempDir(t))
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mutation := range []string{"add", "remove", "change"} {
			t.Run(string(family)+"/"+mutation, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					if family == domain.ProviderOpenAI {
						writeOpenAIAppTurn(w, 1)
					} else {
						writeAnthropicAppTurn(w, 1)
					}
				}))
				defer server.Close()
				root := appPluginFixture(t, "codex")
				sidecar := filepath.Join(root, "skills", "directory-name", "agents", "openai.yaml")
				if mutation == "add" {
					if err := os.Remove(sidecar); err != nil {
						t.Fatal(err)
					}
				}
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture")}}
				dataRoot := filepath.Join(privateAppTempDir(t), "sessions")
				workspace := t.TempDir()
				open := func(thread string) *chatResources {
					resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, thread, false, workspace, chatResourceHooks{pluginDirs: []string{root}})
					if err != nil {
						t.Fatal(err)
					}
					return resources
				}
				live := open("")
				submitAppTurn(t, live, "$codexqa:review frozen")
				a := live.transcriptQueries.projector.(provider.Conversation).ProjectHistory().Turns[0].Messages[0].Skill
				oldDisabled := live.skills[0].Metadata.DisableModelInvocation
				thread := live.identity.ThreadID
				if err := live.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "remove":
					if err := os.Remove(sidecar); err != nil {
						t.Fatal(err)
					}
				case "add", "change":
					value := "false"
					if mutation == "change" {
						value = "true"
					}
					if err := os.WriteFile(sidecar, []byte("policy: {allow_implicit_invocation: "+value+"}\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				restored := open(string(thread))
				defer restored.close(context.Background())
				if restored.history.Turns[0].Messages[0].Skill != a || restored.skills[0].Metadata.DisableModelInvocation == oldDisabled {
					t.Fatal("restore recomputed old instructions or failed to update future policy")
				}
			})
		}
	}
}
