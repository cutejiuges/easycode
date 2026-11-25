//go:build darwin

package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

func TestPromptSubmitDualProviderSteeringStopAndCanonicalRestore(t *testing.T) {
	t.Setenv("HOME", privateAppTempDir(t))
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			workspace := privateAppTempDir(t)
			root := filepath.Join(privateAppTempDir(t), "sessions")
			const fileBody = "PAYLOAD_ONLY_IN_ATTACHMENT"
			writeAppTestFile(t, filepath.Join(workspace, "evidence.txt"), []byte(fileBody), 0600)
			const output = "PRIVATE_PROMPT_HOOK_OUTPUT"
			const feedback = "frozen stop feedback"
			promptScript := `payload=$(cat); case "$payload" in *` + fileBody + `*) exit 1;; *BLOCK_*) printf ` + output + `; printf ` + output + ` >&2; exit 2;; esac`
			stopScript := `payload=$(cat); case "$payload" in *'"stop_hook_active":false'*'prompt stop candidate'*) printf '%s' '{"decision":"block","reason":"` + feedback + `"}' ;; esac`
			body, err := codec.MarshalStable(map[string]any{"hooks": map[string]any{
				"UserPromptSubmit": []map[string]any{{"hooks": []map[string]any{{"id": "submit", "type": "command", "command": promptScript}}}},
				"Stop":             []map[string]any{{"hooks": []map[string]any{{"id": "finish", "type": "command", "command": stopScript, "feedback": "model"}}}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			hookPath := filepath.Join(workspace, ".easycode", "hooks.json")
			if err := os.MkdirAll(filepath.Dir(hookPath), 0700); err != nil {
				t.Fatal(err)
			}
			writeAppTestFile(t, hookPath, body, 0600)

			requests := make(chan []byte, 8)
			gates := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
			var recorder requestRecorder
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index, payload := recorder.record(t, r)
				n := index + 1
				requests <- payload
				w.Header().Set("Content-Type", "text/event-stream")
				if n <= len(gates) {
					select {
					case <-gates[n-1]:
					case <-ctx.Done():
						return
					}
					serveWriteScenarioSample(w, family, n, []writeScenarioCall{{"Write", `{"file_path":"evidence.txt","content":"must not write"}`}})
					return
				}
				answer := "complete"
				if n == 3 {
					answer = "prompt stop candidate"
				}
				serveStopAssistant(w, family, answer)
			}))
			defer server.Close()
			defer cancel()

			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}, true)
			open := func(thread string) *chatResources {
				res := mustOpenChatResourcesWithHooks(t, ctx, cfg, dataPaths{sessionRoot: root}, thread, false, workspace, chatResourceHooks{interactiveApprovals: true})
				t.Cleanup(func() { _ = res.close(context.Background()) })
				if err := res.session.StartInteractive(ctx); err != nil {
					t.Fatal(err)
				}
				return res
			}
			live := open("")
			appendText := func(res *chatResources, text string) domain.InputID {
				result, err := appendInteractiveText(ctx, res.session, text)
				if err != nil || result.Status() != protocol.CommandStatusAccepted {
					t.Fatal("append prompt", result.Status(), err)
				}
				return result.EntryID()
			}
			first := appendText(live, "first @evidence.txt")
			rejected := make(map[domain.InputID]bool)
			final := make(map[domain.InputID]protocol.InputState)
			var wire [][]byte
			for done := false; !done; {
				select {
				case payload := <-requests:
					wire = append(wire, payload)
					switch len(wire) {
					case 1:
						appendText(live, "accepted alpha")
						rejected[appendText(live, "BLOCK_MIXED")] = true
						appendText(live, "accepted omega")
						close(gates[0])
					case 2:
						rejected[appendText(live, "BLOCK_ALL_ORDINARY")] = true
						close(gates[1])
					}
				case item, open := <-live.session.Outputs():
					if !open {
						t.Fatal("prompt output closed before terminal")
					}
					if status, ok := item.InputStatus(); ok && status.State() != protocol.InputClaimed {
						if _, exists := final[status.EntryID()]; exists {
							t.Fatal("duplicate input resolution")
						}
						final[status.EntryID()] = status.State()
					}
					if event, ok := item.TurnEvent(); ok {
						switch event.Kind {
						case protocol.EventHookTrustRequested:
							request, err := protocol.DecodeHookTrustRequested(event)
							if err != nil {
								t.Fatal(err)
							}
							if request.Declaration.Reference.Event == domain.HookStop {
								rejected[appendText(live, "BLOCK_STOP_CORRECTION")] = true
							}
							if err := live.session.ResolveHookTrust(ctx, event.TurnID, request, protocol.HookTrustSession); err != nil {
								t.Fatal(err)
							}
						case protocol.EventTurnFailed:
							t.Fatalf("prompt turn failed: %s", event.Payload)
						case protocol.EventTurnCompleted:
							done = true
						}
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			for len(wire) < len(recorder.snapshot()) {
				wire = append(wire, <-requests)
			}
			if len(wire) != 4 || len(final) != 6 || len(rejected) != 3 || final[first] != protocol.InputCommitted || !bytes.Contains(wire[0], []byte(fileBody)) || !bytes.Contains(wire[3], []byte(feedback)) {
				t.Fatal("lost accepted input, file evidence, or native Stop feedback", len(wire), len(final), len(rejected))
			}
			_, afterAlpha, found := bytes.Cut(wire[1], []byte("accepted alpha"))
			if !found || !bytes.Contains(afterAlpha, []byte("accepted omega")) {
				t.Fatal("mixed steering lost FIFO order")
			}
			for id := range rejected {
				if final[id] != protocol.InputReturned {
					t.Fatal("rejected input was delivered", id)
				}
			}
			for _, payload := range wire {
				if bytes.Contains(payload, []byte("BLOCK_")) || bytes.Contains(payload, []byte(output)) || bytes.Contains(payload, []byte("hook_rejected")) {
					t.Fatal("provider received rejected prompt or hook output")
				}
			}

			appEffortIdle(t, live)
			thread := live.identity.ThreadID
			journalPath, _ := live.service.repository.JournalPath(thread)
			prefix, err := os.ReadFile(journalPath)
			if err != nil || bytes.Contains(prefix, []byte("BLOCK_")) || bytes.Contains(prefix, []byte(output)) || bytes.Contains(prefix, []byte(promptScript)) {
				t.Fatal("prompt hooks persisted private material", err)
			}
			appendText(live, "next unchanged prompt")
			appEffortTerminal(t, live, false)
			liveCache := queryFixtureCacheSnapshot(t, ctx, live, "prompt-cache")
			liveWire := <-requests
			closeAppTestResources(t, live)
			writeAppTestFile(t, journalPath, prefix, 0600)
			if err := os.Remove(hookPath); err != nil {
				t.Fatal(err)
			}
			restored := open(string(thread))
			if len(recorder.snapshot()) != 5 || len(restored.history.Turns) != 1 || strings.Contains(restored.history.Turns[0].Messages[0].Text, "BLOCK_") {
				t.Fatal("recovery reran checks or lost the original native history")
			}
			appendText(restored, "next unchanged prompt")
			appEffortTerminal(t, restored, false)
			restoredCache := queryFixtureCacheSnapshot(t, ctx, restored, "prompt-cache")
			if restoredWire := <-requests; !bytes.Equal(liveWire, restoredWire) || liveCache.StableFingerprint != restoredCache.StableFingerprint || liveCache.LastSample == nil || restoredCache.LastSample == nil || liveCache.LastSample.RequestFingerprint != restoredCache.LastSample.RequestFingerprint || liveCache.LastSample.NativeFingerprint != restoredCache.LastSample.NativeFingerprint {
				t.Fatal("gate metadata changed canonical continuation or cache evidence")
			}
		})
	}
}
