package builtin

import (
	"context"
	"testing"

	"easycode/internal/tool"
)

func TestTaskExecutorBuildsCandidateWithoutHostOrPermissions(t *testing.T) {
	t.Parallel()
	input, err := tool.DecodeUpdatePlanInput([]byte(`{"plan":[{"step":"Check code","status":"pending"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	call, _ := tool.NewTaskReadyCall("task-call", input)
	inv, _ := tool.NewTaskInvocation("018f1d8a-7b5c-7def-8123-456789abcdef", call)
	executor := NewTaskExecutor()
	result := executor.Execute(context.Background(), inv)
	update, applied := result.TaskUpdate()
	if result.Validate() != nil || !applied || !update.Equal(input.Update()) || result.Preview().Text() != "Task progress updated: 0/1 completed" {
		t.Fatal("pure task candidate was not complete")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = executor.Execute(ctx, inv)
	if result.Validate() != nil || result.Status() != tool.ResultCancelled || !result.TaskInput().Equal(input) {
		t.Fatal("cancelled candidate lost its input or terminal")
	}
	if _, applied := result.TaskUpdate(); applied {
		t.Fatal("cancelled candidate applied a list")
	}
	if executor.Execute(absentTaskContext(), inv).Validate() == nil || executor.Execute(context.Background(), tool.TaskInvocation{}).Validate() == nil {
		t.Fatal("invalid executor input accepted")
	}
}

// absentTaskContext 明确构造缺失上下文，以验证边界拒绝而非正常调用路径。
func absentTaskContext() context.Context { return nil }
