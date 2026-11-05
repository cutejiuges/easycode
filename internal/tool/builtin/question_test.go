package builtin

import (
	"context"
	"testing"

	"easycode/internal/tool"
)

type questionAnswerFixture struct {
	answers tool.QuestionAnswerSet
	err     error
}

func (r questionAnswerFixture) Answer(context.Context, tool.QuestionInvocation) (tool.QuestionAnswerSet, error) {
	return r.answers, r.err
}

func TestQuestionExecutorDoesNotGrantOrInventAnswers(t *testing.T) {
	i, err := tool.NewQuestionInput([]tool.Question{{ID: "pick", Header: "Pick", Question: "Pick?", Options: []tool.QuestionOption{{Label: "A", Description: "A"}, {Label: "B", Description: "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := tool.NewQuestionReadyCall("question_call", i)
	v, _ := tool.NewQuestionInvocation("019a0c70-0000-7000-8000-000000000001", c)
	exec := NewQuestionExecutor()
	for _, tc := range []struct {
		name      string
		responder tool.QuestionResponder
		status    tool.ResultStatus
		code      string
	}{
		{"unavailable", nil, tool.ResultDenied, "user_input_unavailable"},
		{"cancelled", questionAnswerFixture{err: context.Canceled}, tool.ResultCancelled, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := exec.Execute(context.Background(), v, tc.responder)
			if r.Validate() != nil || r.Status() != tc.status || r.Code() != tc.code {
				t.Fatal("incorrect rejection", r.Code())
			}
		})
	}
	for _, answer := range []tool.QuestionAnswer{{QuestionID: "pick", SelectedOptions: []int{0}, Text: " notes "}, {QuestionID: "pick", Skipped: true}} {
		a, _ := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{answer})
		r := exec.Execute(context.Background(), v, questionAnswerFixture{answers: a})
		got, err := r.QuestionAnswers()
		if err != nil || got != a || r.Status() != tool.ResultSuccess {
			t.Fatal("lost user decision", err)
		}
	}
}
