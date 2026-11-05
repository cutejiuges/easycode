package provider

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestQuestionRestoreFactsAreTypedAndImmutable(t *testing.T) {
	input, err := tool.NewQuestionInput([]tool.Question{{ID: "choice", Header: "Choice", Question: "Which?", Options: []tool.QuestionOption{{Label: "A", Description: "First"}, {Label: "B", Description: "Second"}}}})
	if err != nil {
		t.Fatal(err)
	}
	ready, _ := tool.NewQuestionReadyCall("q-call", input)
	q, _ := tool.NewQuestionInvocation("01900000-0000-7000-8000-000000000001", ready)
	a, _ := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "choice", SelectedOptions: []int{0}}})
	result, _ := tool.NewQuestionInvocationResult(q, a)
	envelope, _ := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{"kind":"sample"}`))
	record, _ := NewSampleRestoreRecord("01900000-0000-7000-8000-000000000002", envelope)
	questions := []tool.QuestionInvocation{q}
	results := []tool.InvocationResult{result}
	bound, err := record.WithQuestionFacts(questions, results)
	if err != nil {
		t.Fatal(err)
	}
	questions[0] = tool.QuestionInvocation{}
	results[0] = tool.InvocationResult{}
	bound.QuestionInvocations()[0] = tool.QuestionInvocation{}
	bound.QuestionResults()[0] = tool.InvocationResult{}
	if bound.QuestionInvocations()[0] != q || bound.QuestionResults()[0].Preview() != result.Preview() || len(record.QuestionInvocations()) != 0 {
		t.Fatal("mutable restore bindings")
	}
	for _, test := range []struct {
		questions []tool.QuestionInvocation
		results   []tool.InvocationResult
	}{{nil, []tool.InvocationResult{result}}, {[]tool.QuestionInvocation{q, q}, nil}, {[]tool.QuestionInvocation{{}}, nil}, {[]tool.QuestionInvocation{q}, []tool.InvocationResult{result, result}}, {[]tool.QuestionInvocation{q}, []tool.InvocationResult{{}}}} {
		if _, err := record.WithQuestionFacts(test.questions, test.results); err == nil {
			t.Fatal("invalid restore bindings accepted")
		}
	}
	output, _ := NewToolOutputRestoreRecord(record.TurnID(), envelope)
	if _, err := output.WithQuestionFacts([]tool.QuestionInvocation{q}, nil); err == nil {
		t.Fatal("question facts bound to output role")
	}
}
