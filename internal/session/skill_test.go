package session

import (
	"bytes"
	"easycode/internal/domain"
	"easycode/internal/tool"
	"os"
	"testing"
)

func TestSkillCurrentFrozenResultFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/current/skill_result.json")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := DecodeToolCallResultPayload(Record{EventKind: EventToolCallResult, Payload: bytes.TrimSpace(data)})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := tool.NewSkillLoadInput("demo", "args")
	ready, _ := tool.NewSkillReadyCall("call-skill", input)
	invocation, err := tool.NewInvocation("01900000-0000-7000-8000-000000000001", ready)
	if err != nil {
		t.Fatal(err)
	}
	result, err := payload.Domain(invocation)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := result.SkillAttachment()
	if !ok || a.Content != "Frozen instructions" {
		t.Fatal("missing frozen content")
	}
	draft, err := NewToolCallResultDraft(domain.TurnID("01900000-0000-7000-8000-000000000010"), result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(draft.PayloadBytes(), bytes.TrimSpace(data)) {
		t.Fatalf("current fixture changed: %s", draft.PayloadBytes())
	}
	mismatch := payload
	mismatch.SkillMetadata = &SkillResultPayload{Input: SkillInputPayload{Name: "other", Arguments: "args"}, Attachment: &a}
	if _, err := mismatch.Domain(invocation); err == nil {
		t.Fatal("mismatched invocation accepted")
	}
}
