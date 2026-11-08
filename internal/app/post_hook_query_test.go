package app

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
	"errors"
	"strings"
	"testing"
)

func TestPostDetailQueryBindsSnapshotAndKeepsOriginalBody(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	reader := &queryTestReader{}
	entry := queryTestEntry(t, thread, 1)
	declaration, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	plan, _ := domain.NewHookPlan(string(entry.Invocation.InvocationID()), domain.HookExec, []domain.HookReference{declaration.Reference, func() domain.HookReference {
		d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "next", 7, "true", 5)
		return d.Reference
	}()})
	post, _ := domain.NewPostHookPlan(plan, 256<<10, 16<<10, domain.HookDeliveryAvailable)
	entry.Result, _ = tool.FinalizeResult(entry.Result, post.Allowance-post.Reservation, tool.BudgetPreserved)
	q := newTranscriptQueries(thread, reader, []session.ToolHistoryEntry{entry})
	bindQueryTestTranscript(t, q, []session.ToolHistoryEntry{entry})
	defer q.Close()
	snapshot := session.ReplayedPostHooks{Plan: post, PlanSequence: 4}
	if err := q.PostHooksCommitted(entry.Invocation.InvocationID(), 4, snapshot); err != nil {
		t.Fatal(err)
	}
	list, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 1})
	if err != nil || list.Items[0].DetailSequence != 4 || list.Items[0].PostHook.State != protocol.HookActivityChecking || list.Items[0].PostHook.InvocationID != string(entry.Invocation.InvocationID()) {
		t.Fatal("list omitted durable post sequence", err)
	}
	cursor := list.Items[0].Cursor
	oldQuery := protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: cursor, Source: protocol.TranscriptPostHooks, DetailSequence: 4, Limit: 64 << 10}
	initial, err := q.ReadTranscriptBody(context.Background(), oldQuery)
	if err != nil || initial.DetailSequence != 4 || strings.Contains(initial.Text, "explicit body") {
		t.Fatal("wrong planned detail snapshot", err)
	}
	id, _ := domain.NewPostHookRunIdentity(post, 0)
	stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution", StdoutBytes: 100}
	candidate, _ := domain.NewHookFeedbackCandidate("explicit body\n\x1b[31m")
	finished, err := tool.FreezePostFeedback(post, nil, stats, candidate)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Finished = []domain.HookFinished{finished}
	if err := q.PostHooksCommitted(entry.Invocation.InvocationID(), 7, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ReadTranscriptBody(context.Background(), oldQuery); !errors.Is(err, protocol.TranscriptChanged) {
		t.Fatal("mixed post revisions accepted", err)
	}
	fresh := oldQuery
	fresh.DetailSequence = 7
	body, err := q.ReadTranscriptBody(context.Background(), fresh)
	if err != nil || body.DetailSequence != 7 || !strings.Contains(body.Text, "explicit body") || strings.ContainsRune(body.Text, '\x1b') {
		t.Fatal("unsafe authorized detail", err)
	}
	original, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: cursor, Source: protocol.TranscriptResult, Limit: 64 << 10})
	if err != nil || original.Text != "frozen preview" || reader.calls != 0 {
		t.Fatal("post details rewrote original body or read artifact", err)
	}
	if _, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: cursor, Source: protocol.TranscriptPostHooks, Limit: 64 << 10}); err == nil {
		t.Fatal("post query omitted expected sequence")
	}
	// 查询重建后的主线必须继续携带后置终态，不能在原生 outputs 提交时丢失检查提示。
	text, _ := tool.RenderPostFeedback(snapshot.Feedback())
	budget, _ := domain.NewPostHookBudget(post.Allowance, post.Reservation, entry.Result.Budget().ActualBytes(), len(text))
	terminal, err := domain.NewPostHookTerminal(post, "recovery", domain.PostHookCancelled, domain.HookPostInterrupted, 1, nil, budget, domain.HookDigest([]byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Terminal = &terminal
	if err := q.PostHooksCommitted(entry.Invocation.InvocationID(), 9, snapshot); err != nil {
		t.Fatal(err)
	}
	list, err = q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 1})
	if err != nil || list.Items[0].PostHook.State != protocol.HookActivityState(domain.HookCancel) || list.Items[0].PostHook.Code != domain.HookPostInterrupted || list.Items[0].Activity.Status != "success" || list.Items[0].Preview != "frozen preview" {
		t.Fatal("history refresh lost original success or post terminal", err)
	}
}
