package app

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestTaskTranscriptDetailsRemainFrozenAfterReplacementAndClear(t *testing.T) {
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	sid := domain.SessionID("01900000-0000-7000-8000-000000000002")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000003")
	var entries []session.ToolHistoryEntry
	projector := &taskTranscriptProjector{view: domain.TranscriptView{Provider: domain.ProviderOpenAI}}
	var commits []session.NativeCommitRecord
	for i, label := range []string{"任务 A", "失败候选 B", ""} {
		var items []domain.TaskStep
		if label != "" {
			step, _ := domain.NewTaskStep(label, "", domain.TaskPending)
			items = []domain.TaskStep{step}
		}
		update, _ := domain.NewTaskUpdate(items, "说明 "+label)
		input, _ := tool.NewTaskInput(tool.TaskProfileUpdatePlan, update)
		ready, _ := tool.NewTaskReadyCall(tool.ProviderCallID("task-"+string(rune('a'+i))), input)
		id, _ := tool.GenerateInvocationID()
		inv, _ := tool.NewInvocation(id, ready)
		task, _ := inv.Task()
		result, _ := tool.NewTaskInvocationResult(task)
		if i == 1 {
			result = tool.NewInvocationErrorResult(inv, tool.ResultError, "result_budget_exceeded", "Task not updated")
		}
		origin, _ := tool.NewToolCallOrigin(sid, thread, turn, uint32(i), 0)
		seq := uint64(i*5 + 4)
		admission, _ := tool.NewExecutionAdmission(id, seq+1)
		entries = append(entries, session.ToolHistoryEntry{Origin: origin, TurnID: turn, SampleIndex: uint32(i), NativeSequence: seq - 2, Sequence: seq, ResultSequence: seq + 2, Invocation: inv, Admission: admission, Result: result, HasResult: true})
		anchor, _ := domain.NewTranscriptCall(domain.TranscriptPosition{Entry: uint64(i)}, turn, string(inv.ProviderCallID()), "update_plan")
		projector.view.Items = append(projector.view.Items, anchor)
		commits = append(commits, session.NativeCommitRecord{Sequence: seq - 2})
	}
	q := newTranscriptQueries(thread, nil, entries)
	defer q.Close()
	if err := q.bindTranscript(projector, mustAppToolCatalog(t), commits); err != nil {
		t.Fatal(err)
	}
	page, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 10})
	if err != nil || len(page.Items) != 3 {
		t.Fatal("task list projection", err)
	}
	state, err := session.TaskProgressBefore(entries, 0)
	if err != nil || state.Kind() != domain.TaskProgressClear {
		t.Fatal("latest clear projection", err)
	}
	for _, item := range page.Items {
		input, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptInput, Limit: 65536})
		if err != nil {
			t.Fatal(err)
		}
		result, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptResult, Limit: 65536})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(input.Text, `"profile"`) || !strings.Contains(input.Text, `"plan"`) {
			t.Fatal("detail lost real facade", input.Text)
		}
		if strings.Contains(input.Text, "任务 A") {
			if !strings.Contains(result.Text, "任务 A") || strings.Contains(result.Text, "失败候选 B") || !strings.Contains(result.Text, "说明 任务 A") {
				t.Fatal("old detail followed current state", result.Text)
			}
		} else if strings.Contains(input.Text, "失败候选 B") {
			if strings.Contains(result.Text, `"update"`) || strings.Contains(result.Text, "失败候选 B") || !strings.Contains(result.Text, "result_budget_exceeded") {
				t.Fatal("failure candidate claimed applied", result.Text)
			}
		} else if !strings.Contains(result.Text, `"items":[]`) {
			t.Fatal("frozen clear missing", result.Text)
		}
	}
}

// taskTranscriptProjector 只提供本测试冻结的调用锚点。
type taskTranscriptProjector struct{ view domain.TranscriptView }

func (p *taskTranscriptProjector) ProjectTranscript() (domain.TranscriptView, error) {
	return domain.NewTranscriptView(p.view.Provider, p.view.Items)
}
