package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeHookConfig(t *testing.T, workspace, command string) {
	t.Helper()
	dir := filepath.Join(workspace, ".easycode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(command)
	content := fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Edit|apply_patch|Write|Bash|exec_command","hooks":[{"id":"check","type":"command","command":%s,"timeout":1}]}]}}`, b)
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
