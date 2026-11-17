package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/codec"
)

// nestedHookInput 与以下参考类型保留优化前的嵌套 Sonic 调用，不使用新的字节复用路径。
type nestedHookInput HookToolInput

func (v nestedHookInput) MarshalJSON() ([]byte, error) {
	switch v.capability {
	case HookPatch:
		return codec.MarshalStable(v.patch)
	case HookWrite:
		return codec.MarshalStable(v.write)
	case HookExec:
		return codec.MarshalStable(v.exec)
	default:
		return nil, HookConfigInvalid
	}
}

func nestedHookDigest(v HookToolInput) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	b, err := codec.MarshalStable(struct {
		Capability HookCapability  `json:"capability"`
		Input      nestedHookInput `json:"input"`
	}{v.capability, nestedHookInput(v)})
	if err != nil {
		return "", HookConfigInvalid
	}
	return HookDigest(b), nil
}

type nestedHookEffect HookInputEffect

func (v nestedHookEffect) MarshalJSON() ([]byte, error) {
	if v.kind == HookInputUnchanged {
		return codec.MarshalStable(struct {
			Kind   HookInputEffectKind `json:"kind"`
			Before string              `json:"before_digest"`
			After  string              `json:"after_digest"`
		}{v.kind, v.before, v.after})
	}
	if v.kind != HookInputReplaced {
		return nil, HookConfigInvalid
	}
	return codec.MarshalStable(struct {
		Kind   HookInputEffectKind `json:"kind"`
		Before string              `json:"before_digest"`
		After  string              `json:"after_digest"`
		Input  nestedHookInput     `json:"input"`
	}{v.kind, v.before, v.after, nestedHookInput(v.input)})
}

type nestedHookFinished HookFinished

func (v nestedHookFinished) MarshalJSON() ([]byte, error) {
	if v.Result.Identity.Event == HookPreToolUse {
		return codec.MarshalStable(struct {
			Result      HookRunResult      `json:"result"`
			Feedback    FrozenHookFeedback `json:"feedback"`
			InputEffect nestedHookEffect   `json:"input_effect"`
		}{v.Result, v.Feedback, nestedHookEffect(v.InputEffect)})
	}
	return codec.MarshalStable(struct {
		Result   HookRunResult      `json:"result"`
		Feedback FrozenHookFeedback `json:"feedback"`
	}{v.Result, v.Feedback})
}

func hookEncodingFinished(t testing.TB, capability HookCapability) HookFinished {
	t.Helper()
	declaration, err := NewHookDeclaration(HookSourceUser, HookPreToolUse, HookFeedbackNone, HookDigest(nil), "check", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", capability, []HookReference{declaration.Reference})
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewHookRunIdentity(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	result := HookRunResult{Identity: id, Status: HookContinue, Code: HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	feedback, err := NewFrozenHookFeedback(id, declaration.Reference, HookFeedbackEmpty, "", HookOK, 0)
	if err != nil {
		t.Fatal(err)
	}
	return HookFinished{Result: result, Feedback: feedback}
}

func assertHookEncoding(t testing.TB, actual, reference json.Marshaler) {
	t.Helper()
	got, err := actual.MarshalJSON()
	want, wantErr := reference.MarshalJSON()
	if !bytes.Equal(got, want) || fmt.Sprintf("%T:%v", err, err) != fmt.Sprintf("%T:%v", wantErr, wantErr) {
		t.Fatalf("encoding or error classification changed: got %q (%T:%v), want %q (%T:%v)", got, err, err, want, wantErr, wantErr)
	}
	if err == nil && len(got) > 0 {
		got[0] ^= 0xff
		again, err := actual.MarshalJSON()
		if err != nil || !bytes.Equal(again, want) {
			t.Fatal("returned encoding aliases the value", err)
		}
	}
}

func checkHookEncoding(t testing.TB, input HookToolInput, finished HookFinished) {
	t.Helper()
	assertHookEncoding(t, input, nestedHookInput(input))
	digest, err := input.Digest()
	want, wantErr := nestedHookDigest(input)
	if digest != want || err != wantErr {
		t.Fatal("input digest or validation error changed", digest, want, err, wantErr)
	}
	if err != nil {
		digest = HookDigest([]byte("invalid input"))
	}
	effect := HookInputEffect{kind: HookInputReplaced, before: HookDigest([]byte("before")), after: digest, input: input}
	for _, kind := range []HookInputEffectKind{HookInputReplaced, HookInputUnchanged, "invalid"} {
		effect.kind = kind
		assertHookEncoding(t, effect, nestedHookEffect(effect))
		finished.InputEffect = effect
		assertHookEncoding(t, finished, nestedHookFinished(finished))
	}
}

func TestHookEncodingMatchesNestedSonic(t *testing.T) {
	for _, v := range []HookToolInput{
		{capability: HookPatch, patch: hookPatchInput{"中文.txt", []HookReplacement{{"<\n\"", "&\u2028👩‍💻", true, false}}}},
		{capability: HookPatch, patch: hookPatchInput{"f", nil}},
		{capability: HookPatch, patch: hookPatchInput{"f", []HookReplacement{}}},
		{capability: HookWrite, write: hookWriteInput{"f", ""}},
		{capability: HookWrite, write: hookWriteInput{"f", "<>&\"\\\u2028\u2029\n中文\x00"}},
		{capability: HookWrite, write: hookWriteInput{"f", "\xff"}},
		{capability: HookExec, exec: hookExecInput{"echo '<&>'", "", 1000}},
		{capability: HookExec, exec: hookExecInput{"", ".", 0}},
		{},
	} {
		t.Run(fmt.Sprintf("%s-%d", v.capability, len(v.write.Content)+len(v.patch.Replacements)), func(t *testing.T) {
			capability := v.capability
			if !capability.Valid() {
				capability = HookWrite
			}
			checkHookEncoding(t, v, hookEncodingFinished(t, capability))
		})
	}
	for _, event := range []HookEvent{HookPostToolUse, HookPostToolUseFailure, HookStop} {
		t.Run(string(event), func(t *testing.T) {
			finished := hookEncodingFinished(t, HookWrite)
			finished.Result.Identity.Event = event
			finished.Feedback.Identity.Event = event
			if event == HookStop {
				target, err := NewStopHookTarget(AssistantSampleReference{TurnID: "018f1d8a-7b5c-7def-8abc-456789abcdef"})
				if err != nil {
					t.Fatal(err)
				}
				finished.Result.Identity.Target = target
				finished.Feedback.Identity.Target = target
			}
			assertHookEncoding(t, finished, nestedHookFinished(finished))
		})
	}
}

func TestHookDigestPreservesInputAndReplacementLimits(t *testing.T) {
	base := HookToolInput{capability: HookWrite, write: hookWriteInput{"f", ""}}
	empty, _ := base.MarshalJSON()
	for _, limit := range []int{MaxHookRewriteBytes, MaxHookInputBytes} {
		for _, extra := range []int{0, 1} {
			v := base
			v.write.Content = strings.Repeat("x", limit-len(empty)+extra)
			digest, err := v.Digest()
			var wantErr error
			if limit == MaxHookInputBytes && extra == 1 {
				wantErr = HookInputLimit
			}
			if err != wantErr || v.Validate() != wantErr {
				t.Fatal("input limit classification changed", limit, extra, err)
			}
			want, referenceErr := nestedHookDigest(v)
			if digest != want || err != referenceErr {
				t.Fatal("bounded digest changed")
			}
			effect := HookInputEffect{kind: HookInputReplaced, before: HookDigest(nil), after: digest, input: v}
			if (effect.Validate(HookWrite) == nil) != (limit == MaxHookRewriteBytes && extra == 0) {
				t.Fatal("replacement limit changed", limit, extra)
			}
		}
	}
	for _, v := range []HookToolInput{
		{},
		{capability: HookWrite, write: hookWriteInput{"../f", "safe"}},
		{capability: HookWrite, write: hookWriteInput{"f", "\xff"}},
		{capability: HookPatch, patch: hookPatchInput{"f", nil}},
		{capability: HookExec, exec: hookExecInput{" ", "", 1000}},
		{capability: HookExec, exec: hookExecInput{"echo", "", 0}},
	} {
		digest, err := v.Digest()
		if digest != "" || err != HookConfigInvalid || v.Validate() != HookConfigInvalid {
			t.Fatal("invalid input no longer fails closed", err)
		}
	}
}

func FuzzHookEncodingMatchesNestedSonic(f *testing.F) {
	for _, seed := range []string{"", "中文 <>&\u2028\u2029", "\"\\\n\x00", "\xff"} {
		f.Add("f", seed, uint8(0))
	}
	finished := map[HookCapability]HookFinished{}
	for _, capability := range []HookCapability{HookPatch, HookWrite, HookExec} {
		finished[capability] = hookEncodingFinished(f, capability)
	}
	f.Fuzz(func(t *testing.T, file, content string, selector uint8) {
		if len(file)+len(content) > 4096 {
			return
		}
		var input HookToolInput
		switch selector % 3 {
		case 0:
			input = HookToolInput{capability: HookWrite, write: hookWriteInput{file, content}}
		case 1:
			input = HookToolInput{capability: HookPatch, patch: hookPatchInput{file, []HookReplacement{{OldText: content, NewText: "new", ReplaceAll: selector&4 != 0}}}}
		case 2:
			input = HookToolInput{capability: HookExec, exec: hookExecInput{content, file, int(selector)}}
		}
		checkHookEncoding(t, input, finished[input.capability])
	})
}
