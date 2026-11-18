package hook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func hookConfig(id, matcher, command string) string {
	return fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":%q,"hooks":[{"id":%q,"type":"command","command":%q}]}]}}`, matcher, id, command)
}
func putHookConfig(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFreezesSourcesAndInvalidSourceBlocksAllMutations(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	user := filepath.Join(home, ".config", "easycode", "hooks.json")
	project := filepath.Join(root, ".easycode", "hooks.json")
	putHookConfig(t, user, hookConfig("user-check", "Bash", "true"))
	putHookConfig(t, project, hookConfig("project-check", "exec_command", "true"))
	l := NewLoader()
	s, err := l.Load(context.Background(), root, home)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := s.Match(domain.HookPreToolUse, domain.HookExec)
	if err != nil || len(ds) != 2 || ds[0].Reference.Source != domain.HookSourceUser {
		t.Fatal("source order", err)
	}
	putHookConfig(t, project, `{"hooks":{"PreToolUse":null}}`)
	if old, err := s.Match(domain.HookPreToolUse, domain.HookExec); err != nil || len(old) != 2 {
		t.Fatal("snapshot reloaded")
	}
	bad, err := l.Load(context.Background(), root, home)
	if err != nil || len(bad.Diagnostics()) != 1 || len(bad.Declarations()) != 1 {
		t.Fatal("diagnostics lost", err)
	}
	for _, cap := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
		if _, err := bad.Match(domain.HookPreToolUse, cap); err != domain.HookConfigInvalid {
			t.Fatal("partial source allowed")
		}
	}
	putHookConfig(t, project, hookConfig("check", "*", "true"))
	if err := os.Chmod(project, 0660); err != nil {
		t.Fatal(err)
	}
	bad, err = l.Load(context.Background(), root, home)
	if err != nil || len(bad.Diagnostics()) == 0 {
		t.Fatal("writable source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Load(ctx, root, home); err != context.Canceled {
		t.Fatal("cancel ignored")
	}
}
func TestLoadDetectsCaptureChangesAndSourceReplacement(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	p := filepath.Join(root, ".easycode", "hooks.json")
	putHookConfig(t, p, hookConfig("check", "*", "true"))
	l := NewLoader()
	l.afterRead = func() { putHookConfig(t, p, "changed") }
	s, err := l.Load(context.Background(), root, home)
	if err != nil || len(s.Diagnostics()) != 1 {
		t.Fatal("inconsistent capture accepted", err)
	}
	putHookConfig(t, p, hookConfig("check", "*", "true"))
	l.afterRead = nil
	fired := false
	l.beforeOpen = func(current string) {
		if strings.HasSuffix(current, "/.easycode/hooks.json") && !fired {
			fired = true
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "secret"), p); err != nil {
				t.Fatal(err)
			}
		}
	}
	s, err = l.Load(context.Background(), root, home)
	if err != nil || !fired || len(s.Diagnostics()) != 1 {
		t.Fatal("swapped config accepted", err)
	}
}
