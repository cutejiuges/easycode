package tool

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestTaskDetailsPreserveFacadeAndFrozenResult(t *testing.T) {
	for _, test := range []struct {
		profile TaskProfile
		raw     string
	}{
		{TaskProfileTodoWrite, `{"todos":[{"content":"检查内容","activeForm":"正在检查","status":"in_progress"}]}`},
		{TaskProfileUpdatePlan, `{"explanation":"保留说明\n\t继续","plan":[{"step":"检查内容","status":"in_progress"}]}`},
		{TaskProfileUpdatePlan, `{"plan":[]}`},
		{TaskProfileTodoWrite, `{"todos":[]}`},
	} {
		t.Run(string(test.profile)+test.raw, func(t *testing.T) {
			var input TaskInput
			var err error
			if test.profile == TaskProfileTodoWrite {
				input, err = DecodeTodoWriteInput([]byte(test.raw))
			} else {
				input, err = DecodeUpdatePlanInput([]byte(test.raw))
			}
			if err != nil {
				t.Fatal(err)
			}
			text, err := TaskInputText(input)
			if err != nil || text != test.raw || strings.Contains(text, `"profile"`) {
				t.Fatalf("facade detail changed: %s, %v", text, err)
			}
			ready, _ := NewTaskReadyCall("detail-call", input)
			id, _ := ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ac")
			inv, _ := NewTaskInvocation(id, ready)
			success, err := NewTaskInvocationResult(inv)
			if err != nil {
				t.Fatal(err)
			}
			text, err = TaskResultText(success)
			want := `{"status":"success","code":"ok","preview":"` + success.Preview().Text() + `","update":` + string(input.Update().CanonicalJSON()) + `}`
			if err != nil || text != want || strings.Contains(text, `"profile"`) {
				t.Fatalf("successful detail lost full update: %s, %v", text, err)
			}
			failure := newTaskErrorResult(inv, ResultError, "result_budget_exceeded", "Task not updated")
			text, err = TaskResultText(failure)
			if err != nil || text != `{"status":"error","code":"result_budget_exceeded","preview":"Task not updated"}` || strings.Contains(text, "检查内容") {
				t.Fatalf("failed detail claims candidate applied: %s, %v", text, err)
			}
		})
	}
	if _, err := TaskInputText(TaskInput{}); err == nil {
		t.Fatal("zero input detail accepted")
	}
	if _, err := TaskResultText(InvocationResult{}); err == nil {
		t.Fatal("zero result detail accepted")
	}
	step, _ := domain.NewTaskStep("Read", "", domain.TaskPending)
	update, _ := domain.NewTaskUpdate([]domain.TaskStep{step}, "")
	input, _ := NewTaskInput(TaskProfileUpdatePlan, update)
	text, err := TaskInputText(input)
	if err != nil || text != `{"plan":[{"step":"Read","status":"pending"}]}` {
		t.Fatal("absent explanation was added", err)
	}
}
