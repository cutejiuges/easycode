package tool

import (
	"strings"
	"testing"
)

func questionInvocationFixture(t *testing.T) QuestionInvocation {
	t.Helper()
	input, err := NewQuestionInput([]Question{{ID: "storage", Header: "存储", Question: "选择存储？", Options: []QuestionOption{{Label: "Local, fast", Description: "本地"}, {Label: "Remote", Description: "远程"}}}})
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewQuestionReadyCall("call_question", input)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewQuestionInvocation(InvocationID("019a0c70-0000-7000-8000-000000000001"), call)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestQuestionResultFreezesCompleteDecisions(t *testing.T) {
	v := questionInvocationFixture(t)
	a, _ := NewQuestionAnswerSet([]QuestionAnswer{{QuestionID: "storage", SelectedOptions: []int{0}, Text: "  keep commas, and spaces\n"}})
	r, err := NewQuestionInvocationResult(v, a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Preview().Text(), "Local, fast") {
		t.Fatal("lost option label")
	}
	got, err := r.QuestionAnswers()
	if err != nil || got != a {
		t.Fatal("lost answer", err)
	}
	if _, err := FinalizeResult(r, 1024, BudgetPreserved); err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.questionMetadata.AnsweredCount = 0
	if bad.Validate() == nil {
		t.Fatal("accepted forged counts")
	}
	bad = r
	bad.preview = ModelPreview{text: strings.Replace(r.Preview().Text(), "Local, fast", "forged", 1)}
	bad.budget.actual = len(bad.preview.text)
	if bad.Validate() == nil {
		t.Fatal("accepted forged labels")
	}
	input, _ := NewQuestionInput([]Question{{ID: "different", Header: "Other", Question: "Other?", Options: []QuestionOption{{Label: "A", Description: "a"}, {Label: "B", Description: "b"}}}})
	if _, err := DecodeQuestionPreview(input, []byte(r.Preview().Text())); err == nil {
		t.Fatal("accepted wrong question")
	}
}
func TestQuestionToolUnionAndPermission(t *testing.T) {
	v := questionInvocationFixture(t)
	call, _ := NewQuestionReadyCall(v.call, v.input)
	inv, err := NewInvocation(v.id, call)
	if err != nil || inv.Validate() != nil {
		t.Fatal(err)
	}
	if inv.InvocationID() != v.id || inv.ProviderCallID() != v.call || !CapabilityQuestion.Exclusive() || CapabilityQuestion.RequiresApproval() {
		t.Fatal("incorrect identity or scheduling")
	}
	bad := call
	bad.capability = CapabilityRead
	if bad.Validate() == nil {
		t.Fatal("accepted mismatched input")
	}
	bad = call
	bad.readInput = ReadInput{filePath: "x", offset: 1, limit: 1}
	if bad.Validate() == nil {
		t.Fatal("accepted mixed input")
	}
	broken := inv
	broken.capability = CapabilityRead
	if broken.Validate() == nil {
		t.Fatal("accepted mixed invocation")
	}
	r := NewInvocationErrorResult(inv, ResultDenied, "user_input_unavailable", "Interactive input is unavailable")
	if r.Validate() != nil || r.Status() != ResultDenied || r.QuestionMetadata().AnsweredCount != 0 {
		t.Fatal("invalid unavailable result")
	}
	for _, cap := range []CapabilityID{CapabilityPatch, CapabilityWrite, CapabilityExec} {
		if !cap.RequiresApproval() || !cap.Exclusive() {
			t.Fatal("lost approval requirement")
		}
	}
}
