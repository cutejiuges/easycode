package session

import (
	"bytes"
	"context"
	"os"
	"testing"

	"easycode/internal/tool"
)

func replayQuestionInvocation(t *testing.T) tool.Invocation {
	t.Helper()
	in, err := tool.NewQuestionInput([]tool.Question{{ID: "choice", Header: "Choice", Question: "Which option?", Options: []tool.QuestionOption{{Label: "Local", Description: "Local files"}, {Label: "Remote", Description: "Remote service"}}}})
	if err != nil {
		t.Fatal(err)
	}
	ready, _ := tool.NewQuestionReadyCall("call-question", in)
	id, _ := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ad")
	inv, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}
func questionResultDraft(t *testing.T, inv tool.Invocation) RecordDraft {
	t.Helper()
	q, _ := inv.Question()
	a, _ := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "choice", SelectedOptions: []int{0}, Text: " exact\n"}})
	r, err := tool.NewQuestionInvocationResult(q, a)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewToolCallResultDraft(testTurnID, r)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestQuestionReplayIsExclusiveWithoutApproval(t *testing.T) {
	for _, state := range []string{"ready", "started", "result", "duplicate_result", "success_before_start", "approval", "later_started", "terminal_before_result", "forged_input"} {
		t.Run(state, func(t *testing.T) {
			f := newReplayFixture(t)
			f.appendMetadata()
			f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			f.appendInitialInputs(testTurnID)
			inv := replayQuestionInvocation(t)
			if state == "later_started" {
				read := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ae", "call-later")
				f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()), mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)), mustReadyDraftForReplay(t, inv, 0, 0), mustReadyDraftForReplay(t, read, 0, 1))
			} else {
				appendReplayCallSample(t, f, inv)
			}
			switch state {
			case "approval":
				if _, err := NewApprovalMetadata(inv); err == nil {
					t.Fatal("question approval admitted")
				}
				return
			case "started", "result", "duplicate_result", "terminal_before_result", "forged_input":
				f.appendBatch(mustStartedDraftForReplay(t, inv))
			}
			if state == "result" || state == "duplicate_result" || state == "success_before_start" || state == "forged_input" {
				d := questionResultDraft(t, inv)
				if state == "forged_input" {
					q, _ := inv.Question()
					qs := q.Input().Questions()
					qs[0].Header = "Changed"
					input, _ := tool.NewQuestionInput(qs)
					ready, _ := tool.NewQuestionReadyCall(q.ProviderCallID(), input)
					foreign, _ := tool.NewInvocation(inv.InvocationID(), ready)
					f.appendBatch(questionResultDraft(t, foreign))
				} else {
					f.appendBatch(d)
					if state == "duplicate_result" {
						f.appendBatch(d)
					}
				}
			}
			if state == "terminal_before_result" {
				f.appendBatch(mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}))
			}
			if state == "later_started" {
				read := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ae", "call-later")
				f.appendBatch(mustStartedDraftForReplay(t, read))
			}
			plan, err := NewReplayPlanner().Plan(f.loaded())
			valid := state == "ready" || state == "started" || state == "result"
			if !valid {
				if err == nil {
					t.Fatal("illegal question replay accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Tools) != 1 || plan.ToolRecovery.Calls()[0].ApprovalOutcome != "" {
				t.Fatal("question used approval grammar")
			}
			if state == "result" && plan.Tools[0].Result.Preview().Text() == "" {
				t.Fatal("lost answer")
			}
		})
	}
}

func TestQuestionInterruptedLeaseCrashAndMonotonicAppend(t *testing.T) {
	prefix := readCurrentFixture(t, "question_openai_started.jsonl")
	first, err := DecodeRecord(bytes.Split(prefix, []byte{'\n'})[0])
	if err != nil {
		t.Fatal(err)
	}
	root := createLeaseProcessJournal(t, nil)
	repo, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	// fixture 使用统一测试身份，真实子进程在 ready 握手后持续持有 lease。
	if first.ThreadID != testThreadID {
		t.Fatal("unexpected fixture identity")
	}
	path, _ := repo.JournalPath(testThreadID)
	torn := append(bytes.Clone(prefix), []byte(`{"seq":`)...)
	if err := os.WriteFile(path, torn, 0600); err != nil {
		t.Fatal(err)
	}
	helper := startJournalLeaseHelper(t, root)
	if _, err := repo.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatal("competitor bypassed question lease", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, torn) {
		t.Fatal("competitor repaired owned journal")
	}
	helper.kill(t)
	lease, err := repo.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil || len(plan.ToolRecovery.Calls()) != 2 || plan.ToolRecovery.Calls()[1].State != ReplayedToolCallStarted {
		t.Fatal("question crash state lost", err)
	}
	if _, err := NewLoader().Repair(context.Background(), lease, loaded); err != nil {
		t.Fatal(err)
	}
	inv := plan.ToolRecovery.Calls()[1].Invocation
	r := tool.NewInvocationErrorResult(inv, tool.ResultOutcomeUncertain, "outcome_uncertain", "Question outcome is uncertain")
	d, err := NewToolCallResultDraft(plan.ToolRecovery.TurnID(), r)
	if err != nil {
		t.Fatal(err)
	}
	w, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		t.Fatal(err)
	}
	records, err := w.AppendBatch(context.Background(), []RecordDraft{d})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Sequence != loaded.NextSequence {
		t.Fatal("non-monotonic question sequence")
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if !bytes.HasPrefix(got, prefix) {
		t.Fatal("question recovery rewrote facts")
	}
}
