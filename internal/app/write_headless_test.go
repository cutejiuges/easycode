package app

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteHeadlessAllModesRequireApprovalWithoutCreatingDirectories(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(string(family)+"/"+fmt.Sprint(mode), func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					mu.Lock()
					requests = append(requests, body)
					sample := len(requests)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					var calls []writeScenarioCall
					if sample == 1 {
						calls = []writeScenarioCall{{"Write", `{"file_path":"new/pkg/a","content":"must-not-write"}`}}
					}
					serveWriteScenarioSample(w, family, sample, calls)
				}))
				defer server.Close()
				options := Options{Mode: mode, Prompt: "create new/pkg/a", ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions")}
				output := &appWriteCloser{}
				options.Output = output
				if mode == headless.ModeStreamJSON {
					options.Prompt = ""
					options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"write-input\",\"text\":\"create new/pkg/a\"}\n"))
				}
				result := Run(context.Background(), options)
				if result.ExitCode() != 0 {
					t.Fatalf("headless failed: %#v %s", result, output.String())
				}
				if !strings.Contains(output.String(), "answer-2") {
					t.Fatal("正常最终文本丢失", output.String())
				}
				if mode == headless.ModeStreamJSON && (!strings.Contains(output.String(), `"version":1`) || !strings.Contains(output.String(), `"type":"turn.completed"`)) {
					t.Fatal("外部协议改变", output.String())
				}
				if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
					t.Fatal("headless 创建了目录", err)
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(captured) != 2 || !strings.Contains(string(captured[1]), "approval_required") || !strings.Contains(string(captured[1]), "write-1-0") {
					t.Fatalf("缺少配对拒绝输出 %q", captured)
				}
			})
		}
	}
}
