package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	data := readAppTestFile(t, "../session/testdata/current/input_only_openai.jsonl")
	root, _, _ := installPreviewFixture(t, data)
	if err := os.Chmod(filepath.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(filepath.Dir(root), "config.json")
	writeAppTestFile(t, configuration, []byte(`{"provider":"openai","base_url":"https://example.invalid","api_key":"fixture-key","model":"fixture-model"}`), 0600)
	outcome := RunSessionPicker(context.Background(), Options{ConfigPath: configuration, SessionDataRoot: root, SessionCatalogPath: filepath.Join(filepath.Dir(root), "catalog.json"), Input: os.Stdin, Output: os.Stdout}, true)
	fmt.Fprintf(os.Stdout, "\nPICKER_EXIT=%d\n", outcome.ExitCode())
}

func runInChatPickerTerminal(t *testing.T, directory string) {
	t.Helper()
	home := filepath.Join(directory, "home")
	t.Setenv("HOME", home)
	want := 3
	skill := os.Getenv("EASYCODE_PICKER_EXPECT_SKILL") == "1"
	rewind := os.Getenv("EASYCODE_PICKER_EXPECT_REWIND") == "1"
	if skill {
		want++
		path := filepath.Join(home, ".config", "easycode", "skills", "resume", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		writeAppTestFile(t, path, []byte("---\ndescription: Use the local resume skill\n---\ninteractive-resume-skill-body $ARGUMENTS"), 0600)
	}
	if rewind {
		want += 2
	}
	var recorder requestRecorder
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index, body := recorder.record(t, request)
		number := index + 1
		if skill && number == 4 {
			if !strings.Contains(string(body), "interactive-resume-skill-body exact args") {
				t.Error("interactive request omitted the resumed Skill body")
			}
		}
		if rewind && number == 5 {
			if !strings.Contains(string(body), "rewind fixture prompt revised") ||
				strings.Contains(string(body), `"rewind fixture prompt"`) {
				t.Error("rewind submitted unexpected history or draft")
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
		resources := mustOpenChatResources(t, context.Background(), cfg, storage, "", workspace)
		t.Cleanup(func() { _ = resources.close(context.Background()) })
		submitAppTurn(t, resources, name+" prompt")
		if rewind && name == "source" {
			submitAppTurn(t, resources, "rewind fixture prompt")
		}
		threads[name] = string(resources.identity.ThreadID)
		closeAppTestResources(t, resources)
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
	if count := len(recorder.snapshot()); count != want {
		t.Errorf("unexpected Provider request count after session navigation: got %d, want %d", count, want)
	}
	if rewind {
		entries, err := os.ReadDir(filepath.Join(storage, "artifacts"))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if entry.IsDir() {
				count++
			}
		}
		if count != len(threads)+1 {
			t.Errorf("expected exactly one historical branch, got %d thread directories", count)
		}
	}
	fmt.Fprintf(os.Stdout, "\nPICKER_EXIT=%d\n", outcome.ExitCode())
}
