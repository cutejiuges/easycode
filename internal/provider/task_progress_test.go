package provider

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestTaskSummaryWrapperUsesExactContentWithoutSourceIdentity(t *testing.T) {
	summary, _ := domain.NewContextSummary("Generated factual handoff")
	step, _ := domain.NewTaskStep("检查中文代码", "正在检查", domain.TaskInProgress)
	update, _ := domain.NewTaskUpdate([]domain.TaskStep{step}, "")
	source, _ := domain.NewTaskProgressSource(42, "01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002")
	state, _ := domain.NewTaskProgressState(source, update)
	wrapped, err := WrapScopedTaskContextSummary(summary, domain.ManualCompactionScope(), state)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{summary.Text(), "检查中文代码", "正在检查", "in_progress", "historical reference only"} {
		if !strings.Contains(wrapped, want) {
			t.Fatalf("task wrapper omitted %q", want)
		}
	}
	for _, forbidden := range []string{"result_sequence", "turn_id", "invocation_id", "profile", string(source.TurnID()), source.InvocationID()} {
		if strings.Contains(wrapped, forbidden) {
			t.Fatalf("task wrapper disclosed %q", forbidden)
		}
	}
	otherSource, _ := domain.NewTaskProgressSource(99, "01900000-0000-7000-8000-000000000003", "01900000-0000-7000-8000-000000000004")
	other, _ := domain.NewTaskProgressState(otherSource, update)
	again, err := WrapScopedTaskContextSummary(summary, domain.ManualCompactionScope(), other)
	if err != nil || again != wrapped {
		t.Fatal("source identity changed model content", err)
	}
	plain, err := WrapScopedTaskContextSummary(summary, domain.ManualCompactionScope(), domain.NewNoTaskProgress())
	if err != nil || plain != WrapScopedContextSummary(summary, domain.ManualCompactionScope()) {
		t.Fatal("none changed the original handoff", err)
	}
	cleared, _ := domain.NewTaskUpdate([]domain.TaskStep{}, "No remaining steps\nKeep the test note")
	clearState, _ := domain.NewTaskProgressState(source, cleared)
	clearText, err := WrapScopedTaskContextSummary(summary, domain.ManualCompactionScope(), clearState)
	if err != nil || !strings.Contains(clearText, `"kind":"clear"`) || !strings.Contains(clearText, "No remaining steps") || strings.Contains(clearText, step.Content()) {
		t.Fatal("explicit clear lost its exact meaning", err)
	}
	if _, err := WrapScopedTaskContextSummary(summary, domain.ManualCompactionScope(), domain.TaskProgressState{}); err == nil {
		t.Fatal("zero progress state accepted")
	}
}
