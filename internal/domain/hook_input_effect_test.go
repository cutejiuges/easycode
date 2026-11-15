package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestHookInputEffectsVerifyChainAndOwnTheirInput(t *testing.T) {
	context := rewriteContext(t, HookSubstringPatch)
	after, _ := NewHookPatchInput("rewritten.txt", []HookReplacement{{OldText: "a", NewText: "b"}})
	effect, err := NewReplacedHookInputEffect(context.Input(), after, context.Profile())
	if err != nil {
		t.Fatal(err)
	}
	clone := effect.Clone()
	clone.input.patch.Replacements[0].NewText = "wrong"
	if effect.Validate(HookPatch) != nil || clone.Validate(HookPatch) == nil {
		t.Fatal("effect alias or digest unchecked")
	}
	b, _ := codec.MarshalStable(effect)
	restored, err := DecodeHookInputEffect(HookPatch, b)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := restored.ApplyAgainst(context)
	if err != nil || !selected.Equal(after) {
		t.Fatal("chain did not select normalized candidate", err)
	}
	wrong, _ := NewHookRewriteContext(after, HookSubstringPatch)
	if _, err := restored.ApplyAgainst(wrong); err == nil {
		t.Fatal("wrong before digest accepted")
	}
	unchanged, err := NewUnchangedHookInputEffect(context.Input())
	if err != nil || unchanged.Kind() != HookInputUnchanged || unchanged.BeforeDigest() != unchanged.AfterDigest() {
		t.Fatal("invalid unchanged", err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(b, []byte(`"before_digest":`), []byte(`"schema_version":1,"before_digest":`), 1),
		bytes.Replace(b, []byte(`"replace_all":false,`), nil, 1),
		bytes.Replace(b, []byte(`"after_digest":"`), []byte(`"after_digest":"x`), 1),
	} {
		if _, err := DecodeHookInputEffect(HookPatch, bad); err == nil {
			t.Fatal("forged effect accepted", string(bad))
		}
	}
}

func TestPreFinishedRequiresEffectAndRejectsEffectsOnOtherEvents(t *testing.T) {
	context := rewriteContext(t, HookExecProfile)
	after, _ := NewHookExecInput("rewritten", "", 1000)
	effect, _ := NewReplacedHookInputEffect(context.Input(), after, HookExecProfile)
	declaration, _ := NewHookDeclaration(HookSourceUser, HookPreToolUse, HookFeedbackNone, HookDigest(nil), "check", 7, "true", 5)
	plan, _ := NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec, []HookReference{declaration.Reference})
	id, _ := NewHookRunIdentity(plan, 0)
	stats := HookRunResult{Identity: id, Status: HookContinue, Code: HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	finished, err := NewPreHookFinished(stats, declaration.Reference, effect)
	if err != nil {
		t.Fatal(err)
	}
	b, err := codec.MarshalStable(finished)
	if err != nil || len(b) > MaxHookConfigBytes {
		t.Fatal("finished cannot persist", err)
	}
	restored, err := DecodeHookFinished(b)
	if err != nil || restored.InputEffect.AfterDigest() != effect.AfterDigest() {
		t.Fatal("finished lost effect", err)
	}
	bounded := append(append([]byte(nil), b...), bytes.Repeat([]byte(" "), MaxHookConfigBytes-len(b))...)
	if _, err := DecodeHookFinished(bounded); err != nil {
		t.Fatal("exact finished input boundary rejected", err)
	}
	if _, err := DecodeHookFinished(append(bounded, ' ')); err == nil {
		t.Fatal("finished wrapper limit bypassed")
	}
	if _, err := NewEmptyHookFinished(stats, declaration.Reference); err == nil {
		t.Fatal("missing Pre effect accepted")
	}
	for _, code := range []HookCode{HookDenied, HookCancelled, HookOutputInvalid, HookOutcomeUncertain} {
		bad := finished.Clone()
		switch code {
		case HookDenied:
			bad.Result.Status = HookBlock
		case HookCancelled:
			bad.Result.Status = HookCancel
			bad.Result.Cancelled = true
		case HookOutcomeUncertain:
			bad.Result.Status = HookUncertain
			bad.Result.CleanupConfirmed = false
		default:
			bad.Result.Status = HookError
		}
		bad.Result.Code = code
		if bad.Validate() == nil {
			t.Fatal("effect attached to failed run", code)
		}
	}
	for _, event := range []HookEvent{HookPostToolUse, HookPostToolUseFailure, HookStop} {
		bad := finished.Clone()
		bad.Result.Identity.Event = event
		if bad.Validate() == nil {
			t.Fatal("effect attached to another event", event)
		}
	}
	old := []byte(`{"result":`)
	statsBytes, _ := codec.MarshalStable(stats)
	feedbackBytes, _ := codec.MarshalStable(finished.Feedback)
	old = append(old, statsBytes...)
	old = append(old, []byte(`,"feedback":`)...)
	old = append(old, feedbackBytes...)
	old = append(old, '}')
	if _, err := DecodeHookFinished(old); err == nil {
		t.Fatal("old shape read as unchanged")
	}
	if _, err := DecodeHookFinished(bytes.Replace(b, []byte(`"input_effect":`), []byte(`"version":1,"input_effect":`), 1)); err == nil {
		t.Fatal("version accepted")
	}
	large, _ := NewHookWriteInput("f", strings.Repeat("<", 12000))
	if _, err := NewUnchangedHookInputEffect(large); err != nil {
		t.Fatal("unchanged original incorrectly limited to replacement quota")
	}
}
