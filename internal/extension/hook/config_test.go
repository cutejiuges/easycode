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

func TestPreCompactConfigUsesOneFrozenTriggerOrder(t *testing.T) {
	manual := `[{"matcher":"manual","hooks":[{"id":"manual-check","type":"command","command":"true"}]}]`
	stop := `[{"hooks":[{"id":"stop-check","type":"command","command":"true"}]}]`
	user, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreCompact":`+manual+`,"Stop":`+stop+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"Stop":`+stop+`,"PreCompact":`+manual+`}}`))
	if err != nil || len(user) != 2 || len(reordered) != 2 || user[0] != reordered[0] || user[1] != reordered[1] ||
		user[0].Reference.Event != domain.HookStop || user[1].Reference.Feedback != domain.HookFeedbackNone {
		t.Fatal("source order or normalized revision changed", err)
	}
	project, err := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"PreCompact":[
		{"matcher":"auto","hooks":[{"id":"automatic-check","type":"command","command":"true"}]},
		{"matcher":"*","hooks":[{"id":"all-check","type":"command","command":"true","feedback":"model"}]}
	]}}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot(append(user, project...), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		trigger domain.CompactionTrigger
		first   string
	}{
		{domain.CompactionManual, "manual-check"},
		{domain.CompactionAuto, "automatic-check"},
	} {
		checks, err := snapshot.PreCompactChecks(tc.trigger)
		if err != nil || len(checks) != 2 || checks[0].Reference.ID != tc.first || checks[1].Reference.ID != "all-check" {
			t.Fatalf("%s checks = %+v, error: %v", tc.trigger, checks, err)
		}
	}
	nonmatching, err := domain.NewHookSnapshot(user, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checks, err := nonmatching.PreCompactChecks(domain.CompactionAuto); err != nil || len(checks) != 0 {
		t.Fatal("manual matcher affected an automatic operation", err)
	}
	for _, invalid := range []string{strings.Replace(manual, "manual\"", "manual|auto\"", 1), "null"} {
		if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreCompact":`+invalid+`}}`)); err == nil {
			t.Fatal("accepted invalid PreCompact configuration")
		}
	}
	duplicate := strings.Replace(manual, "manual-check", "stop-check", 1)
	if _, err = decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreCompact":`+duplicate+`,"Stop":`+stop+`}}`)); err == nil {
		t.Fatal("accepted an id from another event")
	}
}

func TestPreCompactConfigUsesSharedSourceAndMatchLimits(t *testing.T) {
	var commands []string
	for index := 0; index < 32; index++ {
		commands = append(commands, fmt.Sprintf(`{"id":"check-%d","type":"command","command":"true"}`, index))
	}
	group := func(count int) string {
		return `[{"matcher":"manual","hooks":[` + strings.Join(commands[:count], ",") + `]}]`
	}
	stop := `[{"hooks":[{"id":"stop-check","type":"command","command":"true"}]}]`
	if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreCompact":`+group(32)+`,"Stop":`+stop+`}}`)); err == nil {
		t.Fatal("accepted more than 32 declarations across events")
	}
	declarations, err := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"PreCompact":`+group(domain.MaxHookMatches+1)+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot(declarations, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checks, err := snapshot.PreCompactChecks(domain.CompactionManual); err != domain.HookMatchLimit || len(checks) != 0 {
		t.Fatal("allowed partial execution after matching overflow", err)
	}
	if checks, err := snapshot.PreCompactChecks(domain.CompactionAuto); err != nil || len(checks) != 0 {
		t.Fatal("nonmatching declarations counted against an automatic operation", err)
	}
}
