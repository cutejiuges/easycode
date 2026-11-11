package hook

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestFailureConfigurationSharesStrictFrozenSource(t *testing.T) {
	group := func(id string) string {
		return `[{"matcher":"Bash|exec_command","hooks":[{"id":"` + id + `","type":"command","command":"true","feedback":"model"}]}]`
	}
	pre := `[{"matcher":"*","hooks":[{"id":"pre","type":"command","command":"true"}]}]`
	a, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreToolUse":`+pre+`,"PostToolUse":`+group("success")+`,"PostToolUseFailure":`+group("failure")+`}}`))
	b, other := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PostToolUseFailure":`+group("failure")+`,"PostToolUse":`+group("success")+`,"PreToolUse":`+pre+`}}`))
	if err != nil || other != nil || !reflect.DeepEqual(a, b) || len(a) != 3 {
		t.Fatal("unstable three-event configuration", err, other)
	}
	if a[2].Reference.Event != domain.HookPostToolUseFailure || a[1].Reference.Key() == a[2].Reference.Key() {
		t.Fatal("cross-event identity")
	}
	snapshot, err := domain.NewHookSnapshot(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := snapshot.Match(domain.HookPostToolUseFailure, domain.HookExec)
	if err != nil || len(matches) != 1 {
		t.Fatal("failure alias matched twice", err)
	}
	for _, bad := range []string{
		`{"hooks":{"PostToolUseFailure":null}}`,
		`{"hooks":{"PostToolUse":` + group("same") + `,"PostToolUseFailure":` + group("same") + `}}`,
		`{"hooks":{"PostToolUseFailure":` + strings.Replace(group("failure"), `"model"`, `null`, 1) + `}}`,
	} {
		if _, err := decodeConfig(domain.HookSourceUser, []byte(bad)); err == nil {
			t.Fatal("invalid failure source accepted")
		}
	}
	var commands []string
	for i := 0; i < 31; i++ {
		commands = append(commands, fmt.Sprintf(`{"id":"check-%d","type":"command","command":"true"}`, i))
	}
	large := `[{"matcher":"*","hooks":[` + strings.Join(commands, ",") + `]}]`
	if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreToolUse":`+pre+`,"PostToolUseFailure":`+large+`}}`)); err != nil {
		t.Fatal("32-item source rejected", err)
	}
	if _, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreToolUse":`+pre+`,"PostToolUse":`+group("success")+`,"PostToolUseFailure":`+large+`}}`)); err == nil {
		t.Fatal("combined source limit bypassed")
	}
	for _, count := range []int{8, 9} {
		ds, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PostToolUseFailure":[{"matcher":"*","hooks":[`+strings.Join(commands[:count], ",")+`]}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		s, _ := domain.NewHookSnapshot(ds, nil)
		m, e := s.Match(domain.HookPostToolUseFailure, domain.HookWrite)
		if count == 8 && (e != nil || len(m) != 8) || count == 9 && e != domain.HookMatchLimit {
			t.Fatal("failure match limit", count, e)
		}
	}
}

func TestFailureOutputUsesItsOwnClosedEvent(t *testing.T) {
	for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
		valid := `{"hookSpecificOutput":{"hookEventName":"` + string(event) + `","additionalContext":"diagnostic"}}`
		for _, text := range []string{"", "{}", valid} {
			r := ParseExecution(event, domain.HookFeedbackModel, 0, []byte(text))
			if r.Code != domain.HookOK || r.Status != domain.HookContinue {
				t.Fatal("valid output rejected", event, r)
			}
		}
		for _, text := range []string{
			strings.Replace(valid, string(event), "PreToolUse", 1),
			strings.Replace(valid, `"diagnostic"`, `null`, 1),
			strings.Replace(valid, `"diagnostic"`, `"x","additionalContext":"y"`, 1),
			strings.Replace(valid, `"diagnostic"`, `"`+strings.Repeat("x", 4097)+`"`, 1),
			valid + ` {}`, `{"decision":"block"}`, `{"continue":false}`,
		} {
			if r := ParseExecution(event, domain.HookFeedbackModel, 0, []byte(text)); r.Code != domain.HookOutputInvalid || !r.Feedback.Empty() {
				t.Fatal("invalid failure output accepted", r)
			}
		}
		if r := ParseExecution(event, domain.HookFeedbackNone, 0, []byte(valid)); r.Code != domain.HookFeedbackNotAllowed || !r.Feedback.Empty() {
			t.Fatal("none disclosure", r)
		}
		for _, exit := range []int{1, 2, 255} {
			if r := ParseExecution(event, domain.HookFeedbackModel, exit, []byte(valid)); r.Code != domain.HookExitFailed || !r.Feedback.Empty() {
				t.Fatal("post exit changed", r)
			}
		}
	}
	if r := ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, 2, nil); r.Code != domain.HookDenied {
		t.Fatal("pre exit-two changed")
	}
}
