package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
)

// TestPickerTerminalHelper 供实际 PTY 验收调用，普通测试不启动终端程序。
func TestPickerTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_PICKER_TERMINAL_SMOKE") != "1" {
		return
	}
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
	}
	if directory := os.Getenv("EASYCODE_PICKER_CHAT_FIXTURE"); directory != "" {
		runInChatPickerTerminal(t, directory)
		return
	}
	data, err := os.ReadFile("../session/testdata/current/input_only_openai.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := installPreviewFixture(t, data)
	if err = os.Chmod(filepath.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(filepath.Dir(root), "config.json")
	if err = os.WriteFile(configuration, []byte(`{"provider":"openai","base_url":"https://example.invalid","api_key":"fixture-key","model":"fixture-model"}`), 0600); err != nil {
		t.Fatal(err)
	}
	outcome := RunSessionPicker(context.Background(), Options{ConfigPath: configuration, SessionDataRoot: root, SessionCatalogPath: filepath.Join(filepath.Dir(root), "catalog.json"), Input: os.Stdin, Output: os.Stdout}, true)
	fmt.Fprintf(os.Stdout, "\nPICKER_EXIT=%d\n", outcome.ExitCode())
}

func runInChatPickerTerminal(t *testing.T, directory string) {
	t.Helper()
	home := filepath.Join(directory, "home")
	t.Setenv("HOME", home)
	var want int32 = 3
	if os.Getenv("EASYCODE_PICKER_EXPECT_SKILL") == "1" {
		want++
		path := filepath.Join(home, ".config", "easycode", "skills", "resume", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\ndescription: Use the local resume skill\n---\ninteractive-resume-skill-body $ARGUMENTS"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		number := requests.Add(1)
		if number == 4 && want == 4 {
			body, err := io.ReadAll(request.Body)
			if err != nil || !strings.Contains(string(body), "interactive-resume-skill-body exact args") {
				t.Errorf("interactive request omitted the resumed Skill body: %v", err)
			}
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, int(number))
	}))
	defer server.Close()
	configuration := writeAppConfig(t, directory, domain.ProviderOpenAI, server.URL)
	cfg, err := config.Load(configuration)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	storage := filepath.Join(directory, "sessions")
	threads := make(map[string]string)
	// 先写入三段区分明确的原生历史，再运行同一个真实终端入口。
	for _, name := range []string{"source", "target", "foreign"} {
		workspace := cwd
		if name == "foreign" {
			workspace = t.TempDir()
		}
		resources, err := openChatResources(context.Background(), cfg, storage, "", workspace)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resources.close(context.Background()) })
		submitAppTurn(t, resources, name+" prompt")
		threads[name] = string(resources.identity.ThreadID)
		if err := resources.close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(threads)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stdout, "PICKER_THREADS=%s\n", data)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	options := Options{ConfigPath: configuration, ResumeThreadID: threads["source"], SessionDataRoot: storage,
		SessionCatalogPath: filepath.Join(directory, "catalog.json"), Input: os.Stdin, Output: os.Stdout}
	var outcome Outcome
	if os.Getenv("EASYCODE_PICKER_FROM_STARTUP") == "1" {
		outcome = RunSessionPicker(ctx, options, false)
	} else {
		outcome = Run(ctx, options)
	}
	if count := requests.Load(); count != want {
		t.Errorf("unexpected Provider request count after session navigation: got %d, want %d", count, want)
	}
	fmt.Fprintf(os.Stdout, "\nPICKER_EXIT=%d\n", outcome.ExitCode())
}
