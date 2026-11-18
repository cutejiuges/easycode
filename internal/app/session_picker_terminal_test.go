package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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
