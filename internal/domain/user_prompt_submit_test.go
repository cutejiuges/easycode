package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestUserPromptSubmitStdinLimitsAndOriginalText(t *testing.T) {
	const session = SessionID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	const thread = ThreadID("018f1d8a-7b5c-7def-8abc-456789abcdee")
	anchor := PromptSubmitTarget{
		TurnID: "018f1d8a-7b5c-7def-8abc-456789abcdec", InputID: "018f1d8a-7b5c-7def-8abc-456789abcdeb", SampleIndex: 2,
	}
	const prompt = "  检查 /skill @file\n下一行\x00尾部  "
	cwd := t.TempDir()
	target, err := NewUserPromptSubmitHookTarget(anchor)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := NewUserPromptSubmitHookInput(session, thread, anchor, cwd, prompt)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := payload.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	fields, err := codec.ObjectMembers(encoded)
	var scriptPrompt string
	if err != nil || len(fields) != 7 || codec.UnmarshalRecord(fields["prompt"], &scriptPrompt) != nil || scriptPrompt != prompt {
		t.Fatalf("prompt stdin changed its text or fields: %v", err)
	}
	decoded, err := DecodeHookInput(encoded)
	if err != nil || !decoded.MatchesTarget(target) {
		t.Fatal("restored prompt no longer matches its input", err)
	}
	encodedAgain, err := decoded.Bytes()
	if err != nil || !bytes.Equal(encoded, encodedAgain) {
		t.Fatal("restored stdin changed bytes", err)
	}
	targetBytes, err := target.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeHookTarget(targetBytes)
	if err != nil || restored != target || restored.ValidFor(HookStop) {
		t.Fatal("target lost its sample or spilled into another event", err)
	}
	other := anchor
	other.InputID = InputID(thread)
	otherTarget, err := NewUserPromptSubmitHookTarget(other)
	if err != nil || decoded.MatchesTarget(otherTarget) {
		t.Fatal("another input matched the decoded prompt", err)
	}
	if _, err := DecodeHookInput(append(encoded[:len(encoded)-1], []byte(`,"transcript_path":"private"}`)...)); err == nil {
		t.Fatal("accepted an extra host field")
	}
	if _, err := NewUserPromptSubmitHookInput(session, thread, anchor, cwd, strings.Repeat("\\", MaxHookInputBytes/2)); err != HookInputLimit {
		t.Fatal("encoded byte budget did not include JSON escaping and metadata", err)
	}
}
