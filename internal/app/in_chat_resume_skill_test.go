package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
)

func TestHeadlessResumeNameStillInvokesSkill(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
	}
	path := filepath.Join(workspace, ".easycode", "skills", "resume", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	const body = "resume-named-skill-body $ARGUMENTS"
	if err := os.WriteFile(path, []byte("---\ndescription: Use the local resume skill\n---\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		requests = append(requests, data)
		number := len(requests)
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, number)
	}))
	defer server.Close()
	configuration := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL)
	storage := filepath.Join(privateAppTempDir(t), "sessions")
	const prompt = "/resume exact args"
	var plain bytes.Buffer
	if outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: prompt, ConfigPath: configuration, SessionDataRoot: storage, Output: &plain,
	}); outcome.ExitCode() != 0 || plain.String() != "answer-1\n" {
		t.Fatalf("one-shot result = %#v, %q", outcome, plain.String())
	}
	stream := &appWriteCloser{}
	input := io.NopCloser(strings.NewReader(`{"version":1,"type":"input.submit","request_id":"resume-skill","text":"` + prompt + `"}` + "\n"))
	if outcome := Run(context.Background(), Options{
		Mode: headless.ModeStreamJSON, Input: input, ConfigPath: configuration, SessionDataRoot: storage, Output: stream,
	}); outcome.ExitCode() != 0 || strings.Count(stream.String(), `"type":"turn.completed"`) != 1 {
		t.Fatalf("stream result = %#v, %q", outcome, stream.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("request count = %d", len(requests))
	}
	for index, request := range requests {
		if !bytes.Contains(request, []byte(prompt)) || !bytes.Contains(request, []byte("resume-named-skill-body exact args")) {
			t.Fatalf("request %d omitted the original input or the prepared Skill body", index+1)
		}
	}
}
