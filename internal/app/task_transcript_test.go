package app

import (
	"context"
	"reflect"
	"sort"
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
	projector.end = uint64(len(commits))
	q := newTranscriptQueries(thread, nil)
	defer q.Close()
	if err := q.bindTranscript(projector, mustAppToolCatalog(t), commits, entries, nil); err != nil {
		t.Fatal(err)
	}
	page, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 10})
	if err != nil || len(page.Items) != 3 {
		t.Fatal("task list projection", err)
	}
	if update, applied := entries[len(entries)-1].Result.TaskUpdate(); !applied || update.Counts().Total != 0 {
		t.Fatal("latest task result does not clear its list")
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

// taskTranscriptProjector 提供测试冻结的可见条目和实际提交数。
type taskTranscriptProjector struct {
	view domain.TranscriptView
	end  uint64
}

func (p *taskTranscriptProjector) ProjectTranscriptFrom(start uint64) (domain.TranscriptView, uint64, error) {
	items := p.view.Items
	index := sort.Search(len(items), func(i int) bool { return items[i].Position.Entry >= start })
	view, err := domain.NewTranscriptView(p.view.Provider, items[index:])
	return view, p.end, err
}

func TestTranscriptSuffixPreservesCursorAndRejectsInvalidBatch(t *testing.T) {
	ctx := context.Background()
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000003")
	text, err := domain.NewTranscriptAssistant(domain.TranscriptPosition{}, turn, "repeat")
	if err != nil {
		t.Fatal(err)
	}
	projector := &taskTranscriptProjector{view: domain.TranscriptView{Provider: domain.ProviderOpenAI, Items: []domain.TranscriptItem{text}}, end: 1}
	catalog := mustAppToolCatalog(t)
	q := newTranscriptQueries(thread, nil)
	defer q.Close()
	if err := q.bindTranscript(projector, catalog, []session.NativeCommitRecord{{Sequence: 2}}, nil, nil); err != nil {
		t.Fatal(err)
	}

	projector.end = 2 // 无可见条目的工具 outputs 仍消耗一个原生位置。
	if err := q.NativeCommitted(4, nil); err != nil {
		t.Fatal(err)
	}
	text.Position.Entry = 2
	anchor, err := domain.NewTranscriptCall(domain.TranscriptPosition{Entry: 2, Item: 1}, turn, "call-3", "update_plan")
	if err != nil {
		t.Fatal(err)
	}
	projector.view.Items = append(projector.view.Items, text, anchor)
	projector.end = 3
	query := protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 10}
	if err := q.NativeCommitted(6, nil); err == nil {
		t.Fatal("commit without ready fact was accepted")
	}
	before, err := q.ListTranscript(ctx, query)
	if err != nil || len(before.Items) != 1 || before.Items[0].Cursor.Sequence != 2 {
		t.Fatalf("rejected append changed transcript: %+v, %v", before, err)
	}
	update, _ := domain.NewTaskUpdate(nil, "")
	input, _ := tool.NewTaskInput(tool.TaskProfileUpdatePlan, update)
	ready, _ := tool.NewTaskReadyCall("call-3", input)
	id, _ := tool.GenerateInvocationID()
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	facts := []session.ToolHistoryEntry{{Invocation: invocation, NativeSequence: 6, Sequence: 8, TurnID: turn}}
	if err := q.NativeCommitted(6, facts); err != nil {
		t.Fatal(err)
	}

	restored := newTranscriptQueries(thread, nil)
	defer restored.Close()
	commits := []session.NativeCommitRecord{{Sequence: 2}, {Sequence: 4}, {Sequence: 6}}
	if err := restored.bindTranscript(projector, catalog, commits, facts, nil); err != nil {
		t.Fatal(err)
	}
	live, err := q.ListTranscript(ctx, query)
	want, restoredErr := restored.ListTranscript(ctx, query)
	if err != nil || restoredErr != nil || len(live.Items) != 3 || !reflect.DeepEqual(live, want) ||
		live.Items[0].Cursor != (protocol.TranscriptCursor{Sequence: 6, Item: 1}) ||
		live.Items[1].Text != live.Items[2].Text || live.Items[1].Cursor == live.Items[2].Cursor {
		t.Fatalf("live and restored cursors differ: %+v, %+v, %v, %v", live, want, err, restoredErr)
	}
	query.Before, query.Newer = live.Items[0].Cursor, true
	empty, err := q.ListTranscript(ctx, query)
	if err != nil || len(empty.Items) != 0 || empty.NextBefore.Valid() {
		t.Fatalf("paging beyond latest should return an empty page: %+v, %v", empty, err)
	}
}
