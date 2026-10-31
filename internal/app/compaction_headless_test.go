package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestHeadlessCompactionRealSessionLifecycle(t *testing.T) {
	for _, mode := range []string{"eof", "generation_failure", "cancel", "broken_pipe", "protocol_failure"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body, _ := io.ReadAll(r.Body)
				if !bytes.Contains(body, []byte(`"tool_choice":"none"`)) {
					t.Error("ordinary sampling during compaction")
				}
				if mode == "generation_failure" {
					http.Error(w, "private-provider-secret", 400)
					return
				}
				if mode == "cancel" || mode == "broken_pipe" || mode == "protocol_failure" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, ": ready\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				writeOpenAIAppTurn(w, 2)
			}))
			defer server.Close()
			root := filepath.Join(t.TempDir(), "sessions")
			repository, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			thread := domain.ThreadID("019a39ab-0000-7000-8000-000000000002")
			lease, err := repository.Create(context.Background(), thread)
			if err != nil {
				t.Fatal(err)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			path, _ := repository.JournalPath(thread)
			fixture, err := os.ReadFile("../session/testdata/current/compaction_completed_openai.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(bytes.TrimSuffix(fixture, []byte{'\n'}), []byte{'\n'})
			prefix := append(bytes.Join(lines[:len(lines)-2], []byte{'\n'}), '\n')
			if err := os.WriteFile(path, prefix, 0600); err != nil {
				t.Fatal(err)
			}
			if err := repository.Close(); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "claude-test", BaseURL: server.URL, APIKey: secret.New("private-provider-secret")}}
			resources, err := openChatResources(context.Background(), cfg, root, string(thread), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			inputR, inputW := io.Pipe()
			outputR, outputW := io.Pipe()
			driver := make(chan string, 1)
			go func() {
				defer outputR.Close()
				defer inputW.Close()
				var output strings.Builder
				scanner := bufio.NewScanner(outputR)
				for scanner.Scan() {
					line := scanner.Text()
					output.WriteString(line + "\n")
					var event struct {
						Type string `json:"type"`
						ID   string `json:"compaction_id"`
					}
					_ = json.Unmarshal([]byte(line), &event)
					if event.Type == "thread.started" {
						_, _ = io.WriteString(inputW, `{"version":1,"type":"context.query","request_id":"query"}`+"\n"+`{"version":1,"type":"context.compact","request_id":"compact","instructions":"保留测试"}`+"\n")
						if mode == "eof" || mode == "generation_failure" {
							_ = inputW.Close()
						}
					}
					if event.Type == "compaction.started" {
						switch mode {
						case "cancel":
							_, _ = io.WriteString(inputW, `{"version":1,"type":"context.cancel","request_id":"cancel","expected_compaction_id":"`+event.ID+`"}`+"\n")
							_ = inputW.Close()
						case "protocol_failure":
							_, _ = io.WriteString(inputW, "invalid-json\n")
							_ = inputW.Close()
						case "broken_pipe":
							_ = outputR.Close()
							_, _ = io.WriteString(inputW, `{"version":1,"type":"context.query","request_id":"detect-broken-output"}`+"\n")
							_ = inputW.Close()
						}
					}
				}
				driver <- output.String()
			}()
			outcome := runStreamJSON(context.Background(), Options{Input: inputR, Output: outputW}, resources)
			_ = outputW.Close()
			output := <-driver
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output, "private-provider-secret") || strings.Contains(output, "archived task details") || strings.Contains(output, "answer-2") {
				t.Fatal("private body leaked into control output")
			}
			accepted := strings.Index(output, `"disposition":"compacting"`)
			started := strings.Index(output, `"type":"compaction.started"`)
			if accepted < 0 || started <= accepted {
				t.Fatalf("admission ordering lost: %s", output)
			}
			if mode == "eof" {
				if strings.Count(output, `"type":"compaction.completed"`) != 1 || strings.Contains(output, `"type":"turn.started"`) || requests.Load() != 1 {
					t.Fatalf("EOF did not drain unique compaction: %s", output)
				}
			}
			if mode == "generation_failure" || mode == "cancel" {
				if strings.Count(output, `"type":"compaction.failed"`) != 1 || !strings.Contains(output, `"durable":true`) {
					t.Fatalf("failure did not close durably: %s", output)
				}
			}
			if (mode == "broken_pipe" || mode == "protocol_failure") && outcome.ExitCode() == 0 {
				t.Fatal("transport failure returned success")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.HasPrefix(after, prefix) {
				t.Fatal("session prefix changed")
			}
			// 宿主退出后必须释放 lease，避免输出失败遗留占用。
			repository, err = session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			lease, err = repository.Open(context.Background(), thread)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
		})
	}
}
