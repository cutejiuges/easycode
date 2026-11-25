package app

import (
	"bytes"
	"easycode/internal/domain"
	"path/filepath"
	"testing"
)

// postReadOnlyExample 与 README 的只读包装逐字相同；发现问题仍以 exit 0 返回结构化反馈。
func postReadOnlyExample() string {
	return `cat >/dev/null
if /usr/bin/grep -q TODO main.go; then
  printf '%s' '{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"Read-only check: main.go still contains TODO."}}'
else
  status=$?
  case "$status" in
    1) printf '{}' ;;
    *) exit "$status" ;;
  esac
fi`
}
func writePostHookConfig(t *testing.T, workspace, command string, policy domain.HookFeedbackPolicy) {
	t.Helper()
	writeHookConfig(t, workspace, command)
	path := filepath.Join(workspace, ".easycode", "hooks.json")
	b := readAppTestFile(t, path)
	b = bytes.ReplaceAll(b, []byte(`"PreToolUse"`), []byte(`"PostToolUse"`))
	b = bytes.ReplaceAll(b, []byte(`"type":"command"`), []byte(`"type":"command","feedback":"`+string(policy)+`"`))
	writeAppTestFile(t, path, b, 0600)
}

func writeFailureHookConfig(t *testing.T, workspace, command string, policy domain.HookFeedbackPolicy) {
	t.Helper()
	writePostHookConfig(t, workspace, command, policy)
	path := filepath.Join(workspace, ".easycode", "hooks.json")
	b := readAppTestFile(t, path)
	b = bytes.ReplaceAll(b, []byte(`"PostToolUse"`), []byte(`"PostToolUseFailure"`))
	writeAppTestFile(t, path, b, 0600)
}
