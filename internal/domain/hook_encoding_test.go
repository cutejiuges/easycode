package domain

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestHookEncodingCanonicalBytesAndDigest(t *testing.T) {
	golden, err := os.ReadFile("testdata/hook_encoding.golden")
	if err != nil {
		t.Fatal(err)
	}
	rows := bytes.Split(bytes.TrimSuffix(golden, []byte{'\n'}), []byte{'\n'})
	const content = "中文 <>&\u2028\u2029 \"\\\n"
	inputs := []HookToolInput{
		{capability: HookPatch, patch: hookPatchInput{"f", []HookReplacement{{OldText: content, NewText: "new", ReplaceAll: true}}}},
		{capability: HookWrite, write: hookWriteInput{"f", content}},
		{capability: HookExec, exec: hookExecInput{content, "", 1000}},
	}
	if len(rows) != len(inputs)*5 {
		t.Fatal("incomplete fixed hook encoding fixture")
	}
	for i, input := range inputs {
		t.Run(string(input.capability), func(t *testing.T) {
			digest, err := input.Digest()
			if err != nil || digest != string(rows[i*5]) {
				t.Fatal("hook input digest changed", digest, err)
			}
			effect := HookInputEffect{kind: HookInputReplaced, before: HookDigest([]byte("before")), after: digest, input: input}
			finished := hookEncodingFinished(t, input.capability)
			finished.InputEffect = effect
			unchanged, err := NewUnchangedHookInputEffect(input)
			if err != nil {
				t.Fatal(err)
			}
			for j, value := range []json.Marshaler{input, effect, finished, unchanged} {
				want := rows[i*5+j+1]
				got, err := value.MarshalJSON()
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("canonical hook value %d changed: %s, %v", j, got, err)
				}
				got[0] ^= 0xff
				again, err := value.MarshalJSON()
				if err != nil || !bytes.Equal(again, want) {
					t.Fatal("returned encoding aliases the value", err)
				}
			}
		})
	}
	for _, test := range []struct {
		input HookToolInput
		want  error
	}{
		{HookToolInput{}, HookConfigInvalid},
		{HookToolInput{capability: HookWrite, write: hookWriteInput{"f", "\xff"}}, HookConfigInvalid},
		{HookToolInput{capability: HookExec, exec: hookExecInput{"bad\x00command", "", 1}}, HookConfigInvalid},
		{HookToolInput{capability: HookWrite, write: hookWriteInput{"f", strings.Repeat("x", MaxHookInputBytes)}}, HookInputLimit},
	} {
		if _, err := test.input.Digest(); err != test.want || test.input.Validate() != test.want {
			t.Fatal("invalid input changed error classification", err)
		}
	}
	if _, err := (HookInputEffect{kind: "invalid"}).MarshalJSON(); err != HookConfigInvalid {
		t.Fatal("unknown input effect encoded", err)
	}
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
