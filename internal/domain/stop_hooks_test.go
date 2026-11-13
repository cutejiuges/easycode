package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func stopDomainPlan(t *testing.T, index uint32, count uint8, policies ...HookFeedbackPolicy) StopPlan {
	t.Helper()
	refs := make([]HookReference, len(policies))
	for i, policy := range policies {
		d, err := NewStopHookDeclaration(HookSourceUser, policy, HookDigest(nil), "check-"+string(rune('a'+i)), "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = d.Reference
	}
	p, err := NewStopChecksPlan(AssistantSampleReference{TurnID("018f1d8a-7b5c-7def-8abc-456789abcdef"), index}, count, refs)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func stopDomainFinished(t *testing.T, p StopPlan, ordinal int, status HookOutcome, exit int) HookFinished {
	t.Helper()
	id, err := NewStopHookRunIdentity(p, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	code := HookOK
	if status == HookBlock {
		code = HookDenied
	}
	r := HookRunResult{Identity: id, Status: status, Code: code, ExitKnown: true, ExitCode: exit, CleanupConfirmed: true, Origin: "execution"}
	f, err := NewEmptyHookFinished(r, p.Checks()[ordinal])
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestHookTargetsAndScopesAreClosedAndStrict(t *testing.T) {
	p := stopDomainPlan(t, 0, 0, HookFeedbackModel)
	stop := p.Target()
	tool, err := NewToolHookTarget("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []HookTarget{stop, tool} {
		b, err := codec.MarshalStable(target)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeHookTarget(b)
		if err != nil || got != target {
			t.Fatalf("round trip: %s %v", b, err)
		}
		for _, changed := range [][]byte{bytes.Replace(b, []byte(`"kind":`), []byte(`"schema_version":1,"kind":`), 1), bytes.Replace(b, []byte(`"kind":`), []byte(`"invocation_id":"other","kind":`), 1)} {
			if _, err := DecodeHookTarget(changed); err == nil {
				t.Fatalf("mixed/unknown target accepted: %s", changed)
			}
		}
	}
	if stop.ValidFor(HookPreToolUse) || tool.ValidFor(HookStop) {
		t.Fatal("cross-event target accepted")
	}
	if _, err := NewToolHookTarget("turn", HookExec); err == nil {
		t.Fatal("fake invocation accepted")
	}
	if _, err := NewStopHookTarget(AssistantSampleReference{p.Sample().TurnID, 16}); err == nil {
		t.Fatal("sample overflow accepted")
	}
	for _, raw := range []string{`{"kind":"turn_stop","turn_id":"018f1d8a-7b5c-7def-8abc-456789abcdef"}`, `{"kind":"turn_stop","turn_id":"018f1d8a-7b5c-7def-8abc-456789abcdef","sample_index":null}`, `{"kind":"tool_call","invocation_id":"018f1d8a-7b5c-7def-8abc-456789abcdef","capability":"process.exec","sample_index":0}`} {
		if _, err := DecodeHookTarget([]byte(raw)); err == nil {
			t.Fatal("invalid target accepted", raw)
		}
	}
	for _, raw := range []string{`{"kind":"turn_stop","matcher":0}`, `{"kind":"tool_call"}`, `{"kind":"turn_stop","format_version":1}`} {
		if _, err := DecodeHookScope([]byte(raw)); err == nil {
			t.Fatal("invalid scope accepted", raw)
		}
	}
	id, _ := NewStopHookRunIdentity(p, 0)
	legacy := []byte(`{"event":"Stop","invocation_id":"018f1d8a-7b5c-7def-8abc-456789abcdef","plan_digest":"` + p.Digest() + `","ordinal":0,"run_id":"old"}`)
	var decoded HookRunIdentity
	if codec.UnmarshalRecord(legacy, &decoded) == nil && decoded.Validate() == nil {
		t.Fatal("invocation-only identity accepted")
	}
	if id.InvocationID() != "" || id.Target != stop {
		t.Fatal("stop acquired a tool identity")
	}
}

func TestStopInputPreservesExactSampleAndEncodingLimits(t *testing.T) {
	p := stopDomainPlan(t, 1, 1, HookFeedbackModel)
	uuid := string(p.Sample().TurnID)
	for _, text := range []string{"", "  重复正文\n\n", "\"\\\t雪\n"} {
		in, err := NewStopInput(SessionID(uuid), ThreadID(uuid), p.Sample(), "/workspace", true, text)
		if err != nil {
			t.Fatal(err)
		}
		b, err := in.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeStopInput(b)
		if err != nil || got != in || got.Message() != text {
			t.Fatalf("input changed: %s %v", b, err)
		}
		run, err := NewStopHookInput(in)
		if err != nil || !run.MatchesTarget(p.Target()) {
			t.Fatal("target mismatch", err)
		}
		wire, err := run.Bytes()
		if err != nil || !bytes.Equal(wire, b) {
			t.Fatal("runner input changed", err)
		}
		if bytes.Contains(b, []byte("tool_")) || bytes.Contains(b, []byte("transcript")) {
			t.Fatal("tool field exposed")
		}
		restored, err := DecodeHookInput(b)
		if err != nil || restored.Event != HookStop {
			t.Fatal("runner decoder", err)
		}
		run.ToolUseID = "forged"
		if run.Validate() == nil {
			t.Fatal("mixed input accepted")
		}
		if _, err := DecodeStopInput(bytes.Replace(b, []byte(`"hook_event_name"`), []byte(`"tool_use_id":"forged","hook_event_name"`), 1)); err == nil {
			t.Fatal("unexpected field accepted")
		}
	}
	if _, err := NewStopInput(SessionID(uuid), ThreadID(uuid), p.Sample(), "/workspace", false, strings.Repeat("\x01", MaxHookInputBytes/6)); err != HookInputLimit {
		t.Fatal("escaped size not enforced", err)
	}
	if _, err := NewStopInput(SessionID(uuid), ThreadID(uuid), p.Sample(), "/workspace", false, string([]byte{255})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestStopPlanAndDecisionBindActualBlockingDeclaration(t *testing.T) {
	p := stopDomainPlan(t, 0, 0, HookFeedbackModel, HookFeedbackNone)
	refs := p.Checks()
	refs[0].ID = "changed"
	if p.Checks()[0].ID == "changed" {
		t.Fatal("plan shares slice")
	}
	b, err := codec.MarshalStable(p)
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodeStopPlan(b)
	if err != nil || q.Digest() != p.Digest() {
		t.Fatal("plan round trip", err)
	}
	first := stopDomainFinished(t, p, 0, HookContinue, 0)
	last := stopDomainFinished(t, p, 1, HookBlock, 0)
	candidate, _ := NewHookFeedbackCandidate("fix this")
	if _, err := NewStopFeedback(last.Result, p.Checks()[0], candidate); err != nil {
		// 单独冻结允许相同事件的引用，完整决定仍必须拒绝借用其他声明。
		t.Fatal(err)
	}
	borrowed, _ := NewStopFeedback(last.Result, p.Checks()[0], candidate)
	source, _ := NewStopHookSource(1)
	if _, err := NewStopDecision(p, StopContinued, HookDenied, HookOriginExecution, source, []HookFinished{first, last}, borrowed); err == nil {
		t.Fatal("sibling model authorization accepted")
	}
	model := stopDomainPlan(t, 0, 0, HookFeedbackModel)
	blocked := stopDomainFinished(t, model, 0, HookBlock, 0)
	f, err := NewStopFeedback(blocked.Result, model.Checks()[0], candidate)
	if err != nil {
		t.Fatal(err)
	}
	source, _ = NewStopHookSource(0)
	d, err := NewStopDecision(model, StopContinued, HookDenied, HookOriginExecution, source, []HookFinished{blocked}, f)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.MarshalStable(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeStopDecision(encoded)
	if err != nil || got.ValidateAgainst(model, []HookFinished{blocked}) != nil {
		t.Fatal("decision round trip", err)
	}
	cloned := d.Clone()
	cloned.feedback.Text = "changed"
	if frozen, _ := d.Feedback(); frozen.Text == "changed" {
		t.Fatal("feedback alias")
	}
	for _, mutation := range []func(*StopDecision){func(v *StopDecision) { v.Processed = 0 }, func(v *StopDecision) { v.Continuations = 3 }, func(v *StopDecision) { v.Source = NewStopPlanSource() }, func(v *StopDecision) { v.Code = HookOK }, func(v *StopDecision) { v.Origin = HookOriginRecovery }} {
		bad := d.Clone()
		mutation(&bad)
		if bad.ValidateAgainst(model, []HookFinished{blocked}) == nil {
			t.Fatal("invalid decision accepted")
		}
	}
	for _, raw := range [][]byte{bytes.Replace(encoded, []byte(`"processed":`), []byte(`"version":1,"processed":`), 1), bytes.Replace(encoded, []byte(`"processed":1`), []byte(`"processed":null`), 1)} {
		if _, err := DecodeStopDecision(raw); err == nil {
			t.Fatal("malformed decision accepted")
		}
	}
	badSource := []byte(`{"scope":"plan","ordinal":0}`)
	if _, err := DecodeStopDecisionSource(badSource); err == nil {
		t.Fatal("plan acquired ordinal")
	}
	if _, err := NewStopFeedback(stopDomainFinished(t, model, 0, HookBlock, 2).Result, model.Checks()[0], candidate); err == nil {
		t.Fatal("exit2 continuation accepted")
	}
}

func TestStopDecisionBudgetsAndActualPrefix(t *testing.T) {
	for _, test := range []struct {
		index uint32
		count uint8
		code  HookCode
	}{{15, 0, HookSampleLimitExceeded}, {2, 2, HookStopLimitExceeded}} {
		p := stopDomainPlan(t, test.index, test.count, HookFeedbackModel)
		passed := stopDomainFinished(t, p, 0, HookContinue, 0)
		if _, err := NewStopDecision(p, StopPassed, HookOK, HookOriginExecution, NewStopPlanSource(), []HookFinished{passed}, StopFeedback{}); err != nil {
			t.Fatal("last candidate could not pass", err)
		}
		blocked := stopDomainFinished(t, p, 0, HookBlock, 0)
		candidate, _ := NewHookFeedbackCandidate("repair")
		feedback, _ := NewStopFeedback(blocked.Result, p.Checks()[0], candidate)
		source, _ := NewStopHookSource(0)
		if _, err := NewStopDecision(p, StopContinued, HookDenied, HookOriginExecution, source, []HookFinished{blocked}, feedback); err == nil {
			t.Fatal("over-limit continuation accepted")
		}
		if _, err := NewStopDecision(p, StopBlocked, test.code, HookOriginExecution, source, []HookFinished{blocked}, StopFeedback{}); err != nil {
			t.Fatal("limit decision rejected", err)
		}
	}
	p := stopDomainPlan(t, 0, 0, HookFeedbackModel, HookFeedbackModel)
	if _, err := NewStopDecision(p, StopPassed, HookOK, HookOriginExecution, NewStopPlanSource(), []HookFinished{stopDomainFinished(t, p, 0, HookContinue, 0)}, StopFeedback{}); err == nil {
		t.Fatal("partial pass accepted")
	}
	if _, err := NewStopDecision(p, StopCancelled, HookStopInterrupted, HookOriginRecovery, NewStopPlanSource(), nil, StopFeedback{}); err != nil {
		t.Fatal(err)
	}
	unavailable, err := NewUnavailableStopPlan(p.Sample(), 0, HookConfigInvalid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStopDecision(unavailable, StopError, HookMatchLimit, HookOriginRecovery, NewStopPlanSource(), nil, StopFeedback{}); err == nil {
		t.Fatal("source error changed during recovery")
	}
	if _, err := NewStopChecksPlan(p.Sample(), 0, nil); err == nil {
		t.Fatal("empty checks accepted")
	}
}

func TestStopExecutionDoesNotRelaxOtherEvents(t *testing.T) {
	p := stopDomainPlan(t, 0, 0, HookFeedbackModel)
	blocked := stopDomainFinished(t, p, 0, HookBlock, 0).Result
	c, _ := NewHookFeedbackCandidate("repair")
	if _, err := NewHookExecution(blocked, HookFeedbackModel, c); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*HookRunResult){func(r *HookRunResult) { r.ExitCode = 2 }, func(r *HookRunResult) { r.Status = HookContinue; r.Code = HookOK }, func(r *HookRunResult) { r.Origin = "recovery" }} {
		r := blocked
		mutate(&r)
		if _, err := NewHookExecution(r, HookFeedbackModel, c); err == nil {
			t.Fatal("non-normal block feedback accepted")
		}
	}
	if _, err := NewHookExecution(blocked, HookFeedbackNone, c); err == nil {
		t.Fatal("none disclosed reason")
	}
	for _, event := range []HookEvent{HookPreToolUse, HookPostToolUse, HookPostToolUseFailure} {
		if _, err := NewHookParsedOutput(event, HookFeedbackModel, HookBlock, HookDenied, c); err == nil {
			t.Fatal("tool event received stop block", event)
		}
	}
}

func TestStopContinuationCandidatesKeepRealInputIdentity(t *testing.T) {
	p := stopDomainPlan(t, 0, 0, HookFeedbackModel)
	finished := stopDomainFinished(t, p, 0, HookBlock, 0)
	candidate, _ := NewHookFeedbackCandidate("repair")
	feedback, err := NewStopFeedback(finished.Result, p.Checks()[0], candidate)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := NewUserInput("01900000-0000-7000-8000-000000000002", "  原始纠正\n")
	a, _ := NewInputAssociation(p.Sample().TurnID, InputSteering, 1, []InputID{user.ID()})
	users := []UserInput{user}
	input, err := NewStopWithSteering(feedback, a, users)
	if err != nil {
		t.Fatal(err)
	}
	users[0] = UserInput{}
	returned := input.Inputs()
	returned[0] = UserInput{}
	if input.Validate() != nil || input.Inputs()[0] != user {
		t.Fatal("candidate input alias")
	}
	association, _ := input.Association()
	ids := association.IDs()
	ids[0] = "forged"
	if input.Validate() != nil {
		t.Fatal("candidate association alias")
	}
	if _, err := NewStopWithSteering(feedback, InputAssociation{}, []UserInput{user}); err == nil {
		t.Fatal("missing user association accepted")
	}
	wrong, _ := NewInputAssociation(p.Sample().TurnID, InputSteering, 2, []InputID{user.ID()})
	if _, err := NewStopWithSteering(feedback, wrong, []UserInput{user}); err == nil {
		t.Fatal("wrong target index accepted")
	}
	only, err := NewStopFeedbackOnly(feedback)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := only.Association(); ok || len(only.Inputs()) != 0 {
		t.Fatal("feedback acquired user identity")
	}
}
