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
	invocation, _ := tool.NewInvocation(q.InvocationID(), ready)
	questions := []tool.Invocation{invocation}
	results := []tool.InvocationResult{result}
	bound, err := record.WithReadyFacts(questions, results)
	if err != nil {
		t.Fatal(err)
	}
	questions[0] = tool.Invocation{}
	results[0] = tool.InvocationResult{}
	bound.ReadyInvocations()[0] = tool.Invocation{}
	bound.QuestionResults()[0] = tool.InvocationResult{}
	if !bound.ReadyInvocations()[0].Equal(invocation) || bound.QuestionResults()[0].Preview() != result.Preview() || len(record.ReadyInvocations()) != 0 {
		t.Fatal("mutable restore bindings")
	}
	for _, test := range []struct {
		questions []tool.Invocation
		results   []tool.InvocationResult
	}{{nil, []tool.InvocationResult{result}}, {[]tool.Invocation{invocation, invocation}, nil}, {[]tool.Invocation{{}}, nil}, {[]tool.Invocation{invocation}, []tool.InvocationResult{result, result}}, {[]tool.Invocation{invocation}, []tool.InvocationResult{{}}}} {
		if _, err := record.WithReadyFacts(test.questions, test.results); err == nil {
			t.Fatal("invalid restore bindings accepted")
		}
	}
	output, _ := NewToolOutputRestoreRecord(record.TurnID(), envelope, nil)
	if _, err := output.WithReadyFacts([]tool.Invocation{invocation}, nil); err == nil {
		t.Fatal("question facts bound to output role")
	}
}
