package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestPreCommittedHistoryKeepsOriginalAndRefreshesBoundedDetails(t *testing.T) {
	for _, mode := range []string{"selected", "rejected", "oversized-revert"} {
		t.Run(mode, func(t *testing.T) {
			const thread = domain.ThreadID("01900000-0000-7000-8000-000000000001")
			entry := queryTestEntry(t, thread, 1)
			originalInput, _ := tool.NewExecInput("printf "+strings.Repeat("<", 12000), "", 1000)
			ready, _ := tool.NewExecReadyCall(entry.Invocation.ProviderCallID(), originalInput)
			entry.Invocation, _ = tool.NewInvocation(entry.Invocation.InvocationID(), ready)
			entry.HasResult = false
			entry.Result = tool.InvocationResult{}
			entry.Admission = tool.ExecutionAdmission{}
			q := newTranscriptQueries(thread, nil, []session.ToolHistoryEntry{entry})
			defer q.Close()
			bindQueryTestTranscript(t, q, []session.ToolHistoryEntry{entry})
			before, _ := tool.HookInputForInvocation(entry.Invocation)
			next, _ := domain.NewHookExecInput("printf final", "", 2000)
			refs := make([]domain.HookReference, 2)
			for i, id := range []string{"rewrite", "last"} {
				d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), id, 7, "true", 5)
				refs[i] = d.Reference
			}
			plan, _ := domain.NewHookPlan(string(entry.Invocation.InvocationID()), domain.HookExec, refs)
			g := session.ReplayedHookGate{Plan: plan, PlanSequence: 10}
			if err := q.PreHooksCommitted(entry.Invocation, 10, g); err != nil {
				t.Fatal(err)
			}
			first, _ := domain.NewHookRunIdentity(plan, 0)
			g.Started = &first
			g.StartedSequence = 11
			if err := q.PreHooksCommitted(entry.Invocation, 11, g); err != nil {
				t.Fatal(err)
			}
			effect, _ := domain.NewReplacedHookInputEffect(before, next, domain.HookExecProfile)
			f, err := domain.NewPreHookFinished(domain.HookRunResult{Identity: first, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, refs[0], effect)
			if err != nil {
				t.Fatal(err)
			}
			g.Finished = []domain.HookFinished{f}
			g.Started = nil
			g.StartedSequence = 0
			if err := q.PreHooksCommitted(entry.Invocation, 12, g); err != nil {
				t.Fatal(err)
			}
			page, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Mode: protocol.TranscriptDetail, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			item := page.Items[0]
			if item.PreHook == nil || item.PreHook.PreInput.Kind() != protocol.PreInputCandidate || item.Activity.Status != "waiting" || item.ExecutionAdmitted {
				t.Fatal("candidate claimed selection/execution")
			}
			body, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptPreHooks, DetailSequence: 12, Limit: 64 << 10})
			if err != nil || body.TotalBytes <= 64<<10 || body.NextOffset != 64<<10 {
				t.Fatal("large original was truncated as complete input", err)
			}
			secondBody, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptPreHooks, DetailSequence: 12, Offset: body.NextOffset, Limit: 64 << 10})
			if err != nil || !strings.Contains(secondBody.Text, "Saved candidate C1") || !strings.Contains(secondBody.Text, "not been selected") {
				t.Fatal("candidate audit missing", err)
			}
			second, _ := domain.NewHookRunIdentity(plan, 1)
			g.Started = &second
			g.StartedSequence = 13
			if err := q.PreHooksCommitted(entry.Invocation, 13, g); err != nil {
				t.Fatal(err)
			}
			lastEffect, _ := domain.NewUnchangedHookInputEffect(next)
			if mode == "oversized-revert" {
				_, err = domain.NewReplacedHookInputEffect(next, before, domain.HookExecProfile)
				if err == nil {
					t.Fatal("oversized original cannot be returned through 16 KiB replacement")
				}
				lastEffect, _ = domain.NewUnchangedHookInputEffect(next)
			}
			status, code, block := domain.HookContinue, domain.HookOK, -1
			if mode == "rejected" || mode == "oversized-revert" {
				status, code, block = domain.HookBlock, domain.HookDenied, 1
			}
			stats := domain.HookRunResult{Identity: second, Status: status, Code: code, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
			if status == domain.HookBlock {
				stats.ExitCode = 2
			}
			last, err := domain.NewPreHookFinished(stats, refs[1], lastEffect)
			if err != nil {
				t.Fatal(err)
			}
			g.Finished = append(g.Finished, last)
			g.Started = nil
			g.StartedSequence = 0
			d, err := domain.NewHookGateDecision(plan, status, code, 2, block)
			if err != nil {
				t.Fatal(err)
			}
			g.Decision = &d
			g.DecisionSequence = 15
			if err := q.PreHooksCommitted(entry.Invocation, 15, g); err != nil {
				t.Fatal(err)
			}
			if _, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptPreHooks, DetailSequence: 12, Limit: 64 << 10}); !errors.Is(err, protocol.TranscriptChanged) {
				t.Fatal("old checking page still accepted", err)
			}
			if !q.entries[0].Invocation.Equal(entry.Invocation) || q.entries[0].PreDetailSequence != 15 {
				t.Fatal("live snapshot replaced original or gate sequence")
			}
			copy := g.Clone()
			copy.Finished[0] = domain.HookFinished{}
			if q.entries[0].PreHooks.Finished[0].Validate() != nil {
				t.Fatal("snapshot shares mutable effect prefix")
			}
			final, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Mode: protocol.TranscriptDetail, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if final.Items[0].Cursor != item.Cursor || final.Items[0].PreHook.PreInput.Sequence() != 15 || final.Items[0].ExecutionAdmitted || mode == "selected" && final.Items[0].Activity.Summary != "printf final" {
				t.Fatal("final projection changed anchor/admission/target")
			}
			if err := q.PreHooksCommitted(entry.Invocation, 16, g); err == nil {
				t.Fatal("final gate overwritten by late snapshot")
			}
		})
	}
}
