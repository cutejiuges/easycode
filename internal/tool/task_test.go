package tool

import (
	"bytes"

	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func taskTestInput(t *testing.T, profile TaskProfile) TaskInput {
	t.Helper()
	active := ""
	if profile == TaskProfileTodoWrite {
		active = "Implementing the unique task"
	}
	step, err := domain.NewTaskStep("Implement the unique task", active, domain.TaskInProgress)
	if err != nil {
		t.Fatal(err)
	}
	update, err := domain.NewTaskUpdate([]domain.TaskStep{step}, "")
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewTaskInput(profile, update)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func taskTestInvocation(t *testing.T, profile TaskProfile) Invocation {
	t.Helper()
	call, err := NewTaskReadyCall("task-call", taskTestInput(t, profile))
	if err != nil {
		t.Fatal(err)
	}
	inv, err := NewInvocation("018f1d8a-7b5c-7def-8123-456789abcdef", call)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestTaskFacadeInputsStrictAndBounded(t *testing.T) {
	t.Parallel()
	valid := []struct {
		profile TaskProfile
		raw     string
	}{
		{TaskProfileTodoWrite, `{"todos":[{"content":"检查实现","activeForm":"正在检查实现","status":"in_progress"}]}`},
		{TaskProfileTodoWrite, `{"todos":[]}`},
		{TaskProfileUpdatePlan, `{"plan":[{"step":"Check implementation","status":"pending"}]}`},
		{TaskProfileUpdatePlan, `{"plan":[],"explanation":"Changed\n\tplan"}`},
	}
	for _, tc := range valid {
		decode := DecodeUpdatePlanInput
		if tc.profile == TaskProfileTodoWrite {
			decode = DecodeTodoWriteInput
		}
		v, err := decode([]byte(tc.raw))
		if err != nil || v.Profile() != tc.profile || v.Validate() != nil {
			t.Fatalf("valid input rejected: %v", err)
		}
		if tc.profile == TaskProfileUpdatePlan {
			for _, item := range v.Update().Items() {
				if item.ActiveForm() != "" {
					t.Fatal("OpenAI active form was synthesized")
				}
			}
		}
	}
	invalid := []string{
		`null`, `{"todos":null}`, `{"plan":null}`, `{"plan":[],"plan":[]}`,
		`{"plan":[],"explanation":null}`, `{"plan":[],"other":true}`, `{"plan":[]} {}`,
		`{"todos":[],"explanation":"unexpected"}`, `{"todos":[],"plan":[]}`,
		`{"todos":[{"content":"a","status":"pending"}]}`,
		`{"todos":[{"content":"a","activeForm":"","status":"pending"}]}`,
		`{"todos":[{"content":"a","activeForm":null,"status":"pending"}]}`,
		`{"plan":[{"step":"a","activeForm":"working","status":"pending"}]}`,
		`{"plan":[{"step":"a","status":"pending","status":"completed"}]}`,
		`{"plan":[{"step":"a","status":"unknown"}]}`,
		`{"plan":[{"step":" a","status":"pending"}]}`,
		`{"plan":[{"step":"a\n","status":"pending"}]}`,
		`{"plan":[{"step":"\ud800","status":"pending"}]}`,
		`{"plan":[{"step":"a","status":"pending"},{"step":"a","status":"completed"}]}`,
		`{"plan":[{"step":"a","status":"in_progress"},{"step":"b","status":"in_progress"}]}`,
		`{"plan":[],"explanation":"bad\rcontrol"}`,
		`{"plan":[{"step":"` + strings.Repeat("中", 171) + `","status":"pending"}]}`,
		`{"todos":[{"content":"work","activeForm":"` + strings.Repeat("中", 171) + `","status":"pending"}]}`,
	}
	for _, raw := range invalid {
		if _, err := DecodeTodoWriteInput([]byte(raw)); err == nil {
			t.Fatalf("TodoWrite accepted invalid input: %q", raw)
		}
		if _, err := DecodeUpdatePlanInput([]byte(raw)); err == nil {
			t.Fatalf("update_plan accepted invalid input: %q", raw)
		}
	}
	for _, decode := range []func([]byte) (TaskInput, error){DecodeTodoWriteInput, DecodeUpdatePlanInput} {
		if _, err := decode(append([]byte(`{"plan":[]}`), bytes.Repeat([]byte(" "), MaxTaskInputBytes)...)); err == nil {
			t.Fatal("raw whitespace bypassed the input limit")
		}
		if _, err := decode([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}); err == nil {
			t.Fatal("invalid UTF-8 accepted")
		}
	}
}

func TestTaskFacadeEnforcesCompleteListLimit(t *testing.T) {
	t.Parallel()
	for _, count := range []int{20, 21} {
		var items []string
		for i := 0; i < count; i++ {
			items = append(items, fmt.Sprintf(`{"step":"item %d","status":"pending"}`, i))
		}
		_, err := DecodeUpdatePlanInput([]byte(`{"plan":[` + strings.Join(items, ",") + `]}`))
		if (err == nil) != (count == 20) {
			t.Fatalf("item boundary %d: %v", count, err)
		}
	}
}

func TestTaskResultsAreCompleteBoundedAndRequireAdmission(t *testing.T) {
	t.Parallel()
	inv := taskTestInvocation(t, TaskProfileUpdatePlan)
	task, _ := inv.Task()
	r, err := NewTaskInvocationResult(task)
	if err != nil || ValidateFrozenResult(r) != nil || r.Preview().Text() != "Task progress updated: 0/1 completed" || strings.Contains(r.Preview().Text(), "unique") {
		t.Fatalf("invalid success: %v", err)
	}
	update, ok := r.TaskUpdate()
	if !ok || !update.Equal(task.Input().Update()) || r.Budget().Allowance() != MaxTaskPreviewBytes {
		t.Fatal("task metadata lost or wrong budget")
	}
	origin, _ := NewToolCallOrigin(domain.SessionID(task.id), domain.ThreadID(task.id), domain.TurnID(task.id), 0, 0)
	selection, _ := NewUnhookedInputSelection(origin, inv)
	admission, _ := NewExecutionAdmission(task.id, 10)
	evidence, _ := NewInputResultEvidence(selection, admission, InputResultExecution)
	output, err := NewModelOutput(r, evidence)
	if err != nil || output.Validate() != nil || output.Text() != r.Preview().Text() {
		t.Fatalf("model output: %v", err)
	}
	unadmitted, _ := NewInputResultEvidence(selection, ExecutionAdmission{}, InputResultExecution)
	if _, err := NewModelOutput(r, unadmitted); err == nil {
		t.Fatal("task success without admission accepted")
	}
	if _, err := RestoreTaskInvocationResult(task, ResultCancelled, "cancelled", r.Preview(), update); err == nil {
		t.Fatal("failure with applied metadata accepted")
	}
	badPreview, _ := NewModelPreview("Task progress updated: 1/1 completed")
	if _, err := RestoreTaskInvocationResult(task, ResultSuccess, "ok", badPreview, update); err == nil {
		t.Fatal("forged confirmation accepted")
	}
	failure := NewInvocationErrorResult(inv, ResultCancelled, "cancelled", "Task update cancelled")
	if failure.Validate() != nil || !failure.TaskInput().Equal(task.Input()) {
		t.Fatal("failure lost original input")
	}
	if _, applied := failure.TaskUpdate(); applied {
		t.Fatal("failed result applied tasks")
	}
	if _, err := NewReadonlyResultMaterial(failure); err == nil || !CapabilityTaskProgress.Exclusive() || CapabilityTaskProgress.RequiresApproval() {
		t.Fatal("task entered readonly or approval execution")
	}
	call, _ := inv.ReadyCall()
	if NewApprovalPolicy().Decide(call) != PolicyAllow || NewReadOnlyPolicy().Decide(call) != PolicyDeny {
		t.Fatal("task policy changed safety boundaries")
	}
	if !inv.Equal(inv.Clone()) || !call.TaskInput().Equal(task.Input()) {
		t.Fatal("task call clone changed its identity")
	}
}

func TestTaskCapDoesNotExhaustFollowingRead(t *testing.T) {
	t.Parallel()
	budget := NewResultBudget()
	if err := budget.BeginSample(2); err != nil {
		t.Fatal(err)
	}
	inv, _ := taskTestInvocation(t, TaskProfileUpdatePlan).Task()
	result, _ := NewTaskInvocationResult(inv)
	allowance, err := budget.Allowance(CapabilityTaskProgress)
	if err != nil || allowance != SafetyResultBytes {
		t.Fatal("task did not receive the fixed cap", err)
	}
	result, err = FinalizeResult(result, allowance, BudgetPreserved)
	if err != nil || budget.Commit(CapabilityTaskProgress, result.Budget()) != nil || budget.BodyExhausted() {
		t.Fatal("local task cap exhausted healthy global budget", err)
	}
	readAllowance, err := budget.Allowance(CapabilityRead)
	if err != nil || readAllowance != MaxModelPreviewBytes || budget.TurnUsed() != len(result.Preview().Text()) {
		t.Fatal("following Read lost its body allowance", err)
	}
	oversized, _ := NewResultBudgetMetadata(MaxTaskPreviewBytes+1, len(result.Preview().Text()), BudgetPreserved)
	if _, err := result.WithBudget(oversized); err == nil {
		t.Fatal("restored task budget exceeded its capability cap")
	}
	if _, err := FinalizeResult(result, MaxTaskPreviewBytes+1, BudgetPreserved); err == nil {
		t.Fatal("finalization used another task cap")
	}
}
