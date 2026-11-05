package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
)

func TestQuestionHeadlessAllModesDoNotReadStdin(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(string(family)+"/"+fmt.Sprint(mode), func(t *testing.T) {
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
						name := "request_user_input"
						if family == domain.ProviderAnthropic {
							name = "AskUserQuestion"
						}
						calls = []writeScenarioCall{{name, questionScenarioInput}}
					}
					serveWriteScenarioSample(w, family, sample, calls)
				}))
				defer server.Close()
				out := &appWriteCloser{}
				options := Options{Mode: mode, Prompt: "ask", ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"), SessionCatalogPath: filepath.Join(privateAppTempDir(t), "state.sqlite"), Output: out, Input: questionUnreadInput{t}}
				if mode == headless.ModeStreamJSON {
					options.Prompt = ""
					options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"question-input\",\"text\":\"ask\"}\n"))
				}
				result := Run(context.Background(), options)
				if result.ExitCode() != 0 || !strings.Contains(out.String(), "answer-2") {
					t.Fatal("headless question did not complete", result, out.String())
				}
				if strings.Contains(out.String(), "user_question_requested") || strings.Contains(out.String(), "user_question_resolved") {
					t.Fatal("headless exposed answering channel")
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(captured) != 2 || !strings.Contains(string(captured[1]), "Interactive user input is unavailable") || !strings.Contains(string(captured[1]), "write-1-0") {
					t.Fatal("missing paired unavailable output")
				}
			})
		}
	}
}

type questionUnreadInput struct{ t *testing.T }

func (r questionUnreadInput) Read([]byte) (int, error) {
	r.t.Error("headless question read stdin")
	return 0, io.EOF
}
