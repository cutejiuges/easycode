package app

import (
	"bytes"
	"context"
	"easycode/internal/tui"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPickerProgramExitAndOutputFailure(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		fail        bool
		code        int
	}{{"escape", "\x1b", false, 0}, {"interrupt", "\x03", false, 130}, {"output", "\x03", true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var buffer bytes.Buffer
			var output io.Writer = &buffer
			if tc.fail {
				output = listWriterFunc(func([]byte) (int, error) { return 0, errors.New("injected output failure") })
			}
			result := runPickerProgram(ctx, Options{Input: strings.NewReader(tc.input), Output: output}, tui.PickerConfig{}, func(ctx context.Context, r tui.PickerRequest) pickerExecution {
				return pickerExecution{result: tui.PickerResult{Request: r}}
			})
			if result.ExitCode() != tc.code {
				t.Fatalf("got %d want %d: %+v", result.ExitCode(), tc.code, result)
			}
		})
	}
}

// TestPickerTerminalHelper 供实际 PTY 验收调用，普通测试不启动终端程序。
func TestPickerTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_PICKER_TERMINAL_SMOKE") != "1" {
		return
	}
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
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
	outcome := RunSessionPicker(context.Background(), Options{ConfigPath: configuration, SessionDataRoot: root, SessionCatalogPath: filepath.Join(filepath.Dir(root), "state.sqlite"), Input: os.Stdin, Output: os.Stdout}, true)
	fmt.Fprintf(os.Stdout, "\nPICKER_EXIT=%d\n", outcome.ExitCode())
}
