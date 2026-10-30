package app

import (
	"bytes"
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteSSEChunksCancellationEOFAndLimitsPrecedeSideEffects(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, scenario := range []string{"random-chunks", "one-byte", "half-packet", "ready-without-terminal", "cancel-partial", "invalid-input", "oversize-input", "aggregate-limit"} {
			t.Run(string(family)+"/"+scenario, func(t *testing.T) {
				root := t.TempDir()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				var samples atomic.Int32
				partialSent := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					sample := int(samples.Add(1))
					w.Header().Set("Content-Type", "text/event-stream")
					if sample > 1 {
						serveWriteScenarioSample(w, family, sample, nil)
						return
					}
					input := `{"file_path":"new/pkg/a","content":"中文\r\n"}`
					if scenario == "invalid-input" {
						input = `{"file_path":"new/pkg/a","content":null}`
					}
					if scenario == "oversize-input" {
						input = `{"file_path":"new/pkg/a","content":"` + strings.Repeat("x", 1<<20) + `"}`
					}
					calls := []writeScenarioCall{{"Write", input}}
					if scenario == "aggregate-limit" {
						calls = nil
						for i := 0; i < 17; i++ {
							calls = append(calls, writeScenarioCall{"Write", fmt.Sprintf(`{"file_path":"new/%d","content":%q}`, i, strings.Repeat("x", (1<<20)-100))})
						}
					}
					var buffer bytes.Buffer
					serveWriteScenarioSample(&buffer, family, 1, calls)
					body := buffer.Bytes()
					switch scenario {
					case "half-packet", "cancel-partial":
						body = body[:len(body)/2]
					case "ready-without-terminal":
						if family == domain.ProviderOpenAI {
							body = body[:bytes.LastIndex(body, []byte("data: {\"type\":\"response.completed\""))]
						} else {
							body = body[:bytes.LastIndex(body, []byte("data: {\"type\":\"message_stop\""))]
						}
					}
					random := rand.New(rand.NewSource(17))
					for len(body) > 0 {
						size := len(body)
						if scenario == "one-byte" {
							size = 1
						} else if scenario == "random-chunks" {
							size = min(len(body), 1+random.Intn(31))
						}
						if _, err := w.Write(body[:size]); err != nil {
							return
						}
						body = body[size:]
						if scenario == "one-byte" || scenario == "random-chunks" {
							w.(http.Flusher).Flush()
						}
					}
					if scenario == "cancel-partial" {
						w.(http.Flusher).Flush()
						close(partialSent)
						<-r.Context().Done()
					}
				}))
				defer server.Close()
				model := "gpt-test"
				if family == domain.ProviderAnthropic {
					model = "claude-test"
				}
				resources, err := openChatResourcesWithHooks(context.Background(), config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("stream-fixture"), Model: model}}, dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}, "", false, root, chatResourceHooks{interactiveApprovals: true})
				if err != nil {
					t.Fatal(err)
				}
				control, err := startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				events, err := submitInteractiveFixture(t, control, ctx, "write")
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "cancel-partial" {
					<-partialSent
					control.Interrupt()
				}
				approvals, completed, failed := 0, 0, 0
				for event := range events {
					switch event.Kind {
					case protocol.EventToolApprovalRequested:
						approvals++
						p, err := protocol.DecodeToolApprovalRequested(event)
						if err != nil {
							t.Fatal(err)
						}
						if err := control.ResolveApproval(context.Background(), event.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
							t.Fatal(err)
						}
					case protocol.EventTurnCompleted:
						completed++
					case protocol.EventTurnFailed:
						failed++
					}
				}
				if err := control.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				valid := scenario == "one-byte" || scenario == "random-chunks"
				if valid {
					data, err := os.ReadFile(filepath.Join(root, "new/pkg/a"))
					if err != nil || string(data) != "中文\r\n" || approvals != 1 || completed != 1 || failed != 0 {
						t.Fatalf("有效 chunk 流丢失字节/结果 %q %v %d/%d/%d", data, err, approvals, completed, failed)
					}
				} else {
					if approvals != 0 || completed != 0 || failed != 1 {
						t.Fatalf("无效流进入批准 %d/%d/%d", approvals, completed, failed)
					}
					entries, err := os.ReadDir(root)
					if err != nil || len(entries) != 0 {
						t.Fatal("无效流产生文件副作用", entries, err)
					}
				}
			})
		}
	}
}
