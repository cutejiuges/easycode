package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func TestStopDetailsBindExactTargetPlanSequenceAndKeepIndependentCursor(t *testing.T) {
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	turn := domain.TurnID(thread)
	d, _ := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	plan, _ := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: turn}, 0, []domain.HookReference{d.Reference})
	q := newTranscriptQueries(thread, nil, nil)
	defer q.Close()
	item, _ := domain.NewTranscriptAssistant(domain.TranscriptPosition{Entry: 1}, turn, "saved answer")
	cursor := protocol.TranscriptCursor{Sequence: 6}
	q.nodes = []transcriptNode{{cursor: cursor, item: item, toolIndex: -1}}
	state := session.ReplayedStop{Plan: plan, PlanSequence: 8, DetailSequence: 8}
	if err := q.StopHooksCommitted(state); err != nil {
		t.Fatal(err)
	}
	query := protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: cursor, Source: protocol.TranscriptStopHooks, DetailSequence: 8, StopHook: &protocol.StopHookQueryTarget{Target: plan.Target(), PlanDigest: plan.Digest()}, Limit: 64 << 10}
	body, err := q.ReadTranscriptBody(context.Background(), query)
	if err != nil || body.DetailSequence != 8 || strings.Contains(body.Text, "Authorized model feedback") {
		t.Fatal("unaccepted feedback visible", err)
	}
	id, _ := domain.NewStopHookRunIdentity(plan, 0)
	result := domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution", StdoutBytes: 80, StderrBytes: 90}
	finished, _ := domain.NewEmptyHookFinished(result, d.Reference)
	candidate, _ := domain.NewHookFeedbackCandidate("authorized reason\n\x1b[31m")
	feedback, _ := domain.NewStopFeedback(result, d.Reference, candidate)
	source, _ := domain.NewStopHookSource(0)
	decision, err := domain.NewStopDecision(plan, domain.StopContinued, domain.HookDenied, domain.HookOriginExecution, source, []domain.HookFinished{finished}, feedback)
	if err != nil {
		t.Fatal(err)
	}
	state.Finished = []domain.HookFinished{finished}
	state.Decision = &decision
	state.DecisionSequence = 11
	state.DetailSequence = 11
	if err := q.StopHooksCommitted(state); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ReadTranscriptBody(context.Background(), query); !errors.Is(err, protocol.TranscriptChanged) {
		t.Fatal("mixed detail sequence accepted", err)
	}
	query.DetailSequence = 11
	body, err = q.ReadTranscriptBody(context.Background(), query)
	if err != nil || !strings.Contains(body.Text, feedback.Text) || strings.ContainsRune(body.Text, '\x1b') {
		t.Fatal("frozen disclosure changed or terminal control escaped", err)
	}
	for _, field := range []string{"target", "plan"} {
		bad := query
		binding := *query.StopHook
		bad.StopHook = &binding
		if field == "target" {
			sample := plan.Sample()
			sample.SampleIndex++
			binding.Target, _ = domain.NewStopHookTarget(sample)
		} else {
			binding.PlanDigest = domain.HookDigest([]byte("other"))
		}
		if _, err := q.ReadTranscriptBody(context.Background(), bad); !errors.Is(err, protocol.TranscriptChanged) {
			t.Fatal("cross source detail accepted", field, err)
		}
	}
	before, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
	if err != nil || len(before.Items) != 1 || before.Items[0].Cursor != cursor || before.Items[0].StopHook == nil {
		t.Fatal("intent created a fake cursor", err)
	}
	before.Items[0].StopHook.PlanDigest = "changed"
	again, _ := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
	if again.Items[0].StopHook.PlanDigest != plan.Digest() {
		t.Fatal("list snapshot alias")
	}
	hook, _ := domain.NewTranscriptHook(domain.TranscriptPosition{Entry: 2}, feedback)
	state.DeliverySequence, state.DetailSequence = 12, 12
	if err := q.StopHooksCommitted(state); err != nil {
		t.Fatal(err)
	}
	q.nodes = append(q.nodes, transcriptNode{cursor: protocol.TranscriptCursor{Sequence: 12}, item: hook, toolIndex: -1})
	list, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
	if err != nil || len(list.Items) != 2 || list.Items[0].Kind != domain.TranscriptHook || list.Items[0].InputID != "" || list.Items[0].HookSource == nil {
		t.Fatal("missing independent Hook source", err)
	}
	list.Items[0].HookSource.Reference.ID = "changed"
	text, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: list.Items[0].Cursor, Source: protocol.TranscriptText, Limit: 64 << 10})
	if err != nil || text.Text != feedback.Text {
		t.Fatal("native feedback source alias or rerender", err)
	}
	original, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: cursor, Source: protocol.TranscriptText, Limit: 64 << 10})
	if err != nil || original.Text != "saved answer" {
		t.Fatal("feedback overwrote original answer", err)
	}
}
