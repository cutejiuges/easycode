package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/secret"
)

func TestFileHeadlessLiteralModesNeverReadOrSearch(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(string(family)+"/"+string(mode), func(t *testing.T) {
				bodies := make(chan []byte, 2)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					bodies <- body
					w.Header().Set("Content-Type", "text/event-stream")
					serveStopAssistant(w, family, "literal complete")
				}))
				defer server.Close()
				cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("fixture-key")}}
				fixture, err := os.ReadFile("../session/testdata/current/files_input_" + string(family) + ".jsonl")
				if err != nil {
					t.Fatal(err)
				}
				root, _, thread := installPreviewFixture(t, fixture)
				r, err := openChatResources(context.Background(), cfg, root, string(thread), privateAppTempDir(t))
				if err != nil {
					t.Fatal(err)
				}
				defer r.close(context.Background())
				boundary := &countingFileBoundary{workspaceFileBoundary: r.workspace}
				r.fileSource.workspace = boundary
				const literal = "literal @missing @../outside"
				output := &appWriteCloser{}
				if mode == headless.ModeStreamJSON {
					outcome := runStreamJSON(context.Background(), Options{Input: io.NopCloser(strings.NewReader(`{"version":1,"type":"input.submit","request_id":"literal-files","text":"` + literal + `"}` + "\n")), Output: output}, r)
					if outcome.Class != ExitSuccess {
						t.Fatal("stream literal failed", output.String())
					}
				} else {
					if err := r.session.Start(context.Background()); err != nil {
						t.Fatal(err)
					}
					result := headless.Run(context.Background(), r.session, headless.RunConfig{Mode: mode, Prompt: literal, SessionID: r.identity.SessionID, ThreadID: r.identity.ThreadID, Output: output})
					if !result.Completed {
						t.Fatal("one-shot literal failed", output.String())
					}
				}
				body := <-bodies
				if boundary.reads.Load() != 0 || boundary.scans.Load() != 0 || !bytes.Contains(body, []byte(literal)) || bytes.Count(body, []byte("# Attached workspace file")) != 2 || !bytes.Contains(body, []byte("@not-recursive")) || strings.Contains(output.String(), `"files"`) || strings.Contains(output.String(), "file_reference") || strings.Contains(output.String(), "user_input_committed") {
					t.Fatal("headless activated internal file protocol", output.String())
				}
			})
		}
	}
}
