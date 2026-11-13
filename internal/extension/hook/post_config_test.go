package hook

import (
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestBothEventConfigurationAndDisclosure(t *testing.T) {
	pre := `[{"matcher":"Write","hooks":[{"id":"pre","type":"command","command":"true"}]}]`
	post := `[{"matcher":"Bash|exec_command","hooks":[{"id":"post","type":"command","command":"true"}]}]`
	parse := func(text string) []domain.HookDeclaration {
		t.Helper()
		ds, err := decodeConfig(domain.HookSourceUser, []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return ds
	}
	a := parse(`{"hooks":{"PreToolUse":` + pre + `,"PostToolUse":` + post + `}}`)
	b := parse(`{"hooks":{"PostToolUse":` + post + `,"PreToolUse":` + pre + `}}`)
	if len(a) != 2 || len(b) != 2 || a[0] != b[0] || a[1] != b[1] || a[1].Reference.Feedback != domain.HookFeedbackNone {
		t.Fatal("unstable source or default disclosure")
	}
	model := parse(`{"hooks":{"PreToolUse":` + pre + `,"PostToolUse":` + strings.Replace(post, `"id":"post"`, `"id":"post","feedback":"model"`, 1) + `}}`)
	if model[1].Reference.Fingerprint == a[1].Reference.Fingerprint || model[0].Reference.Revision == a[0].Reference.Revision {
		t.Fatal("disclosure did not invalidate identity")
	}
	s, _ := domain.NewHookSnapshot(a, nil)
	matched, err := s.Match(domain.HookPostToolUse, domain.HookExec)
	if err != nil || len(matched) != 1 || matched[0].Reference.ID != "post" {
		t.Fatal("event alias match", err)
	}
	for _, bad := range []string{
		`{"hooks":{"PreToolUse":` + strings.Replace(pre, `"id":"pre"`, `"id":"pre","feedback":"none"`, 1) + `}}`,
		`{"hooks":{"PostToolUse":` + strings.Replace(post, `"id":"post"`, `"id":"post","feedback":"unknown"`, 1) + `}}`,
		`{"hooks":{"PreToolUse":` + pre + `,"PostToolUse":` + strings.Replace(post, `"post"`, `"pre"`, 1) + `}}`,
		`{"hooks":{"PostToolUse":null}}`, `{"hooks":{"StopFailure":[]}}`,
	} {
		if _, err := decodeConfig(domain.HookSourceUser, []byte(bad)); err == nil {
			t.Fatal("invalid event configuration accepted", bad)
		}
	}
}

func TestCombinedEventLimits(t *testing.T) {
	group := func(start, count int) string {
		var commands []string
		for i := start; i < start+count; i++ {
			commands = append(commands, fmt.Sprintf(`{"id":"check-%d","type":"command","command":"true"}`, i))
		}
		return `[{"matcher":"*","hooks":[` + strings.Join(commands, ",") + `]}]`
	}
	config := func(a, b int) string {
		return `{"hooks":{"PreToolUse":` + group(0, a) + `,"PostToolUse":` + group(a, b) + `}}`
	}
	ds, err := decodeConfig(domain.HookSourceUser, []byte(config(16, 16)))
	if err != nil || len(ds) != 32 {
		t.Fatal("combined boundary", err)
	}
	project, err := decodeConfig(domain.HookSourceProject, []byte(config(16, 16)))
	if err != nil {
		t.Fatal(err)
	}
	combined := append(append([]domain.HookDeclaration(nil), ds...), project...)
	if snapshot, err := domain.NewHookSnapshot(combined, nil); err != nil || len(snapshot.Declarations()) != 64 {
		t.Fatal("two-source boundary", err)
	}
	if _, err := domain.NewHookSnapshot(append(combined, project[0]), nil); err == nil {
		t.Fatal("two-source overflow accepted")
	}
	if _, err := decodeConfig(domain.HookSourceUser, []byte(config(16, 17))); err == nil {
		t.Fatal("combined source overflow accepted")
	}
	ds, err = decodeConfig(domain.HookSourceUser, []byte(config(8, 8)))
	if err != nil {
		t.Fatal(err)
	}
	s, err := domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []domain.HookEvent{domain.HookPreToolUse, domain.HookPostToolUse} {
		matches, err := s.Match(event, domain.HookWrite)
		if err != nil || len(matches) != 8 {
			t.Fatal("per-event boundary", err)
		}
	}
	ds, err = decodeConfig(domain.HookSourceUser, []byte(config(8, 9)))
	if err != nil {
		t.Fatal(err)
	}
	s, err = domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Match(domain.HookPostToolUse, domain.HookWrite); err != domain.HookMatchLimit {
		t.Fatal("per-event overflow accepted", err)
	}
	if matches, err := s.Match(domain.HookPreToolUse, domain.HookWrite); err != nil || len(matches) != 8 {
		t.Fatal("cross-event limit leak", err)
	}
}
