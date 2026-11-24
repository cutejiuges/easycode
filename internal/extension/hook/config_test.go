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

func TestCompactionHookConfigUsesOneFrozenTriggerOrder(t *testing.T) {
	manual := `[{"matcher":"manual","hooks":[{"id":"manual-check","type":"command","command":"true"}]}]`
	postManual := strings.Replace(manual, "manual-check", "manual-observer", 1)
	postManualExplicit := strings.Replace(postManual, `"command":"true"`, `"command":"true","feedback":"none"`, 1)
	stop := `[{"hooks":[{"id":"stop-check","type":"command","command":"true"}]}]`
	user, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreCompact":`+manual+`,"Stop":`+stop+`,"PostCompact":`+postManual+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PostCompact":`+postManualExplicit+`,"Stop":`+stop+`,"PreCompact":`+manual+`}}`))
	if err != nil || len(user) != 3 || len(reordered) != 3 || user[0] != reordered[0] || user[1] != reordered[1] || user[2] != reordered[2] ||
		user[0].Reference.Event != domain.HookStop || user[1].Reference.Event != domain.HookPreCompact ||
		user[2].Reference.Event != domain.HookPostCompact || user[2].Reference.Feedback != domain.HookFeedbackNone {
		t.Fatal("source order or normalized revision changed", err)
	}
	project, err := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"PreCompact":[
		{"matcher":"auto","hooks":[{"id":"automatic-check","type":"command","command":"true"}]},
		{"matcher":"*","hooks":[{"id":"all-check","type":"command","command":"true","feedback":"model"}]}
	],"PostCompact":[
		{"matcher":"auto","hooks":[{"id":"automatic-observer","type":"command","command":"true"}]},
		{"matcher":"*","hooks":[{"id":"all-observer","type":"command","command":"true"}]}
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
		pre     string
		post    string
	}{
		{domain.CompactionManual, "manual-check", "manual-observer"},
		{domain.CompactionAuto, "automatic-check", "automatic-observer"},
	} {
		checks, err := snapshot.PreCompactChecks(tc.trigger)
		if err != nil || len(checks) != 2 || checks[0].Reference.ID != tc.pre || checks[1].Reference.ID != "all-check" {
			t.Fatalf("%s checks = %+v, error: %v", tc.trigger, checks, err)
		}
		observers, err := snapshot.PostCompactChecks(tc.trigger)
		if err != nil || len(observers) != 2 || observers[0].Reference.ID != tc.post || observers[1].Reference.ID != "all-observer" {
			t.Fatalf("%s observers = %+v, error: %v", tc.trigger, observers, err)
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
	for _, invalid := range []string{
		manual,
		strings.Replace(postManual, `"manual"`, `"manual|auto"`, 1),
		strings.Replace(postManualExplicit, `"none"`, `"model"`, 1),
	} {
		if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreCompact":`+manual+`,"PostCompact":`+invalid+`}}`)); err == nil {
			t.Fatal("accepted invalid PostCompact declaration")
		}
	}
}

func TestCompactionHookConfigUsesSharedSourceAndMatchLimits(t *testing.T) {
	var commands []string
	for index := 0; index < 32; index++ {
		commands = append(commands, fmt.Sprintf(`{"id":"check-%d","type":"command","command":"true"}`, index))
	}
	group := func(count int) string {
		return `[{"matcher":"manual","hooks":[` + strings.Join(commands[:count], ",") + `]}]`
	}
	stop := `[{"hooks":[{"id":"stop-check","type":"command","command":"true"}]}]`
	if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PostCompact":`+group(32)+`,"Stop":`+stop+`}}`)); err == nil {
		t.Fatal("accepted more than 32 declarations across events")
	}
	declarations, err := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"PostCompact":`+group(domain.MaxHookMatches+1)+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot(declarations, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checks, err := snapshot.PostCompactChecks(domain.CompactionManual); err != domain.HookMatchLimit || len(checks) != 0 {
		t.Fatal("allowed partial execution after matching overflow", err)
	}
	if checks, err := snapshot.PostCompactChecks(domain.CompactionAuto); err != nil || len(checks) != 0 {
		t.Fatal("nonmatching declarations counted against an automatic operation", err)
	}
	startup := func(count int) string { return strings.Replace(group(count), `"manual"`, `"startup"`, 1) }
	if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"SessionStart":`+startup(32)+`,"Stop":`+stop+`}}`)); err == nil {
		t.Fatal("SessionStart exceeded the shared declaration limit")
	}
	starts, err := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"SessionStart":`+startup(domain.MaxHookMatches+1)+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = domain.NewHookSnapshot(starts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checks, err := snapshot.SessionStartChecks(domain.SessionStartStartup); err != domain.HookMatchLimit || len(checks) != 0 {
		t.Fatal("SessionStart selected a partial plan after matching overflow", err)
	}
}

func TestSessionStartConfigFreezesOrderAndTrustIdentity(t *testing.T) {
	const start = `[{"matcher":"*","hooks":[{"id":"all","type":"command","command":"true","feedback":"model"}]}]`
	const resume = `[{"matcher":"resume","hooks":[{"id":"resume-only","type":"command","command":"true"}]}]`
	const post = `[{"matcher":"auto","hooks":[{"id":"inspect","type":"command","command":"true"}]}]`
	payload := `{"hooks":{"SessionStart":` + start + `,"PostCompact":` + post + `}}`
	user, err := decodeConfig(domain.HookSourceUser, []byte(payload))
	reordered, reorderedErr := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PostCompact":`+post+`,"SessionStart":`+start+`}}`))
	project, projectErr := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"SessionStart":`+resume+`}}`))
	if err != nil || reorderedErr != nil || projectErr != nil || len(user) != 2 || len(reordered) != 2 ||
		user[0].Reference.Event != domain.HookPostCompact || user[1] != reordered[1] {
		t.Fatalf("unexpected normalized order or config: %v, %v, %v", err, reorderedErr, projectErr)
	}
	snapshot, err := domain.NewHookSnapshot(append(user, project...), nil)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := snapshot.SessionStartChecks(domain.SessionStartResume)
	compact, compactErr := snapshot.SessionStartChecks(domain.SessionStartCompact)
	if err != nil || compactErr != nil || len(activation) != 2 || len(compact) != 1 ||
		activation[0] != compact[0] || activation[1].Reference.ID != "resume-only" {
		t.Fatal("incorrect source or wildcard matching", err, compactErr)
	}
	for _, edit := range [][2]string{{`"*"`, `"resume"`}, {`"model"`, `"none"`}, {`"true","feedback"`, `"false","feedback"`}} {
		changed, err := decodeConfig(domain.HookSourceUser, []byte(strings.Replace(payload, edit[0], edit[1], 1)))
		if err != nil || len(changed) != 2 || changed[1].Reference.Key() == user[1].Reference.Key() {
			t.Fatal("reused trust after declaration changed", err)
		}
	}
	for _, invalid := range []string{"clear", "resume|compact", "auto"} {
		if _, err := decodeConfig(domain.HookSourceUser, []byte(strings.Replace(payload, `"*"`, fmt.Sprintf("%q", invalid), 1))); err == nil {
			t.Fatal("accepted unsupported SessionStart source", invalid)
		}
	}
}
