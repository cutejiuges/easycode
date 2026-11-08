package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestHookEventsIsolatePlansRunsAndTrust(t *testing.T) {
	makeDeclaration := func(event HookEvent, feedback HookFeedbackPolicy) HookDeclaration {
		t.Helper()
		d, err := NewHookDeclaration(HookSourceProject, event, feedback, strings.Repeat("a", 64), "check", 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	pre, post := makeDeclaration(HookPreToolUse, HookFeedbackNone), makeDeclaration(HookPostToolUse, HookFeedbackNone)
	model := makeDeclaration(HookPostToolUse, HookFeedbackModel)
	if pre.Reference.Key() == post.Reference.Key() || post.Reference.Key() == model.Reference.Key() {
		t.Fatal("event or disclosure reused trust")
	}
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	pp, _ := NewHookPlan(id, HookExec, []HookReference{pre.Reference})
	qp, _ := NewHookPlan(id, HookExec, []HookReference{post.Reference})
	a, _ := NewHookRunIdentity(pp, 0)
	b, _ := NewHookRunIdentity(qp, 0)
	if a == b || a.RunID == b.RunID || pp.Digest == qp.Digest {
		t.Fatal("phase identities collided")
	}
	b.Event = HookPreToolUse
	if b.Validate() == nil {
		t.Fatal("cross-event run accepted")
	}
	if _, err := NewHookPlan(id, HookExec, []HookReference{pre.Reference, model.Reference}); err == nil {
		t.Fatal("mixed plan accepted")
	}
	if _, err := NewHookDeclaration(HookSourceProject, HookPreToolUse, HookFeedbackModel, strings.Repeat("a", 64), "bad", 7, "true", 5); err == nil {
		t.Fatal("pre disclosure accepted")
	}
	if _, err := NewHookDeclaration(HookSourceProject, "Unknown", HookFeedbackNone, strings.Repeat("a", 64), "bad", 7, "true", 5); err == nil {
		t.Fatal("unknown event accepted")
	}
	encoded, _ := codec.MarshalStable(pp)
	if _, err := DecodeHookPlan(bytes.Replace(encoded, []byte(`"event":"PreToolUse"`), []byte(`"event":"PostToolUse"`), 1)); err == nil {
		t.Fatal("cross-event plan accepted")
	}
	old, _ := codec.MarshalStable(a)
	if codec.UnmarshalRecord(bytes.Replace(old, []byte(`"event":"PreToolUse",`), nil, 1), &b) == nil && b.Validate() == nil {
		t.Fatal("old run shape accepted")
	}
}

func TestHookExitTwoValidationDependsOnEvent(t *testing.T) {
	for _, event := range []HookEvent{HookPreToolUse, HookPostToolUse} {
		d, _ := NewHookDeclaration(HookSourceProject, event, HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "true", 5)
		p, _ := NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec, []HookReference{d.Reference})
		id, _ := NewHookRunIdentity(p, 0)
		r := HookRunResult{Identity: id, Status: HookError, Code: HookExitFailed, ExitKnown: true, ExitCode: 2, CleanupConfirmed: true, Origin: "execution"}
		if (r.Validate() == nil) != (event == HookPostToolUse) {
			t.Fatal("wrong exit-two classification", event)
		}
		r.Status, r.Code = HookBlock, HookDenied
		if (r.Validate() == nil) != (event == HookPreToolUse) {
			t.Fatal("cross-event deny accepted", event)
		}
	}
}
