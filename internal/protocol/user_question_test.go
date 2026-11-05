package protocol

import (
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestQuestionControlIsStrictAndSeparateFromApproval(t *testing.T) {
	input, _ := NewQuestionInput(questionFixture())
	p := UserQuestionRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000001", InvocationID: "01900000-0000-7000-8000-000000000002", Fingerprint: input.Fingerprint(), Input: input, ResultAllowance: MaxQuestionPreviewBytes}
	e, err := NewUserQuestionRequested(p)
	if err != nil {
		t.Fatal(err)
	}
	e.SessionID = "01900000-0000-7000-8000-000000000003"
	e.ThreadID = "01900000-0000-7000-8000-000000000004"
	e.TurnID = "01900000-0000-7000-8000-000000000005"
	if e.Validate() != nil {
		t.Fatal("valid question event rejected")
	}
	got, err := DecodeUserQuestionRequested(e)
	if err != nil || got != p {
		t.Fatal("question event roundtrip", err)
	}
	a, _ := NewQuestionAnswerSet([]QuestionAnswer{{QuestionID: "storage", Text: " user answer\n"}})
	response, err := NewUserQuestionResponse(p.RequestID, p.InvocationID, p.Fingerprint, a)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := codec.MarshalStable(response)
	decoded, err := DecodeUserQuestionResponse(b)
	if err != nil || decoded != response {
		t.Fatal("response roundtrip", err)
	}
	if _, err := DecodeUserQuestionResponse([]byte(strings.Replace(string(b), `"fingerprint":`, `"allow_session":true,"fingerprint":`, 1))); err == nil {
		t.Fatal("answer grants permission")
	}
	c, err := NewResolveUserQuestionCommand("answer", e.TurnID, response)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewAcceptedCommandResult(c, CommandDispositionResolving)
	if err != nil || result.Validate() != nil {
		t.Fatal("answer admission", err)
	}
	c.text = "unrelated"
	if c.Validate() == nil {
		t.Fatal("accepted answer as input")
	}
	for _, bad := range []UserQuestionResponse{{RequestID: p.RequestID, InvocationID: p.InvocationID, Fingerprint: "invalid", Answers: a}, {RequestID: "old", InvocationID: p.InvocationID, Fingerprint: p.Fingerprint, Answers: a}} {
		if bad.Validate() == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	for _, resolution := range []QuestionResolution{QuestionAnswered, QuestionSkipped, QuestionCancelled} {
		resolved, err := NewUserQuestionResolved(UserQuestionResolvedPayload{p.RequestID, p.InvocationID, p.Fingerprint, resolution})
		if err != nil {
			t.Fatal(err)
		}
		resolved.SessionID = e.SessionID
		resolved.ThreadID = e.ThreadID
		resolved.TurnID = e.TurnID
		if resolved.Validate() != nil {
			t.Fatal("resolution rejected")
		}
	}
	if _, err := NewResolveUserQuestionCommand("answer", domain.TurnID("old"), response); err == nil {
		t.Fatal("stale turn format accepted")
	}
	for _, code := range []ControlErrorCode{ControlErrorNoActiveQuestion, ControlErrorQuestionMismatch, ControlErrorQuestionAnswerInvalid} {
		if _, ok := controlErrorMessage(code); !ok {
			t.Fatal("missing safe control error")
		}
	}
}
