package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
)

func TestQuestionAcceptedAnswerRetainsShutdownOwnerUntilSync(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	journal := &fakeJournal{hook: func(ds []session.RecordDraft) {
		for _, d := range ds {
			if d.EventKind() == session.EventToolCallResult {
				close(entered)
				<-release
			}
		}
	}}
	r, c := runtimeQuestionFixture(t, journal)
	host := NewChatSession(r)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	requested := make(chan protocol.Event, 1)
	resolved := make(chan protocol.Event, 1)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for item := range host.Outputs() {
			if e, ok := item.TurnEvent(); ok {
				switch e.Kind {
				case protocol.EventUserQuestionRequested:
					requested <- e
				case protocol.EventUserQuestionResolved:
					resolved <- e
				}
			}
		}
	}()
	if _, err := host.Append(ctx, "ask"); err != nil {
		t.Fatal(err)
	}
	var e protocol.Event
	select {
	case e = <-requested:
	case <-ctx.Done():
		t.Fatal("question not requested")
	}
	p, _ := protocol.DecodeUserQuestionRequested(e)
	wrong := p
	wrong.RequestID = "01900000-0000-7000-8000-000000000009"
	if err := host.ResolveQuestion(ctx, e.TurnID, wrong, questionResponse(t, p, "accepted").Answers); err == nil {
		t.Fatal("stale request admitted")
	}
	if err := host.ResolveQuestion(ctx, e.TurnID, p, questionResponse(t, p, "accepted").Answers); err != nil {
		t.Fatal(err)
	}
	<-entered
	select {
	case <-resolved:
		t.Fatal("resolution before Sync")
	default:
	}
	stop, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	if err := host.Shutdown(stop); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown abandoned blocked owner", err)
	}
	select {
	case <-host.Done():
		t.Fatal("shutdown completed before result Sync")
	default:
	}
	close(release)
	if err := host.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	<-drained
	if len(c.results) != 1 {
		t.Fatal("accepted answer not paired")
	}
	a, err := c.results[0].QuestionAnswers()
	if err != nil || a.Answers()[0].Text != "accepted" {
		t.Fatal("shutdown discarded accepted answer", err)
	}
}

func runtimeQuestionFixture(t *testing.T, journal Journal) (*Runtime, *fakeToolConversation) {
	t.Helper()
	input, err := tool.NewQuestionInput([]tool.Question{{ID: "choice", Header: "Choice", Question: "Choose?", Options: []tool.QuestionOption{{Label: "Yes", Description: "Choice only"}, {Label: "Other", Description: "Another choice"}}}})
	if err != nil {
		t.Fatal(err)
	}
	call, _ := tool.NewQuestionReadyCall("question-call", input)
	first, last := newPreparedSampleWithReady(t, call), newPreparedSample(t, func() {})
	base := &fakeConversation{}
	base.stream = func(ctx context.Context, in provider.TurnInput) (<-chan provider.StreamEvent, error) {
		sample := first
		if base.calls.Load() > 1 {
			sample = last
		}
		return fixedStream(completedProviderEventWithSample(t, sample))(ctx, in)
	}
	c := &fakeToolConversation{fakeConversation: base}
	config := testRuntimeConfig(t, journal)
	config.InteractiveQuestions = true
	config.QuestionExecutor = builtin.NewQuestionExecutor()
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	r, err := New(c, config)
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}
func questionResponse(t *testing.T, p protocol.UserQuestionRequestedPayload, text string) protocol.UserQuestionResponse {
	t.Helper()
	a, err := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "choice", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := protocol.NewUserQuestionResponse(p.RequestID, p.InvocationID, p.Fingerprint, a)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func TestQuestionResponseCancellationAndDurability(t *testing.T) {
	for _, mode := range []string{"answered", "cancel_before_answer", "cancel_after_answer", "sync_failure", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			journal := &fakeJournal{}
			if mode == "sync_failure" {
				journal.failAt = 5
			}
			r, c := runtimeQuestionFixture(t, journal)
			if mode == "unavailable" {
				r.interactiveQuestions = false
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requested, resolved := 0, 0
			err := r.RunTurn(ctx, runtimeInputs("ask"), func(e protocol.Event) {
				switch e.Kind {
				case protocol.EventUserQuestionRequested:
					requested++
					p, err := protocol.DecodeUserQuestionRequested(e)
					if err != nil {
						t.Error(err)
						return
					}
					for _, batch := range journal.snapshot() {
						for _, draft := range batch {
							if draft.EventKind() == session.EventToolCallResult {
								t.Error("result before response")
							}
						}
					}
					if mode == "cancel_before_answer" {
						cancel()
						if code := r.acceptQuestionAnswer(e.TurnID, questionResponse(t, p, "answer")); code != protocol.ControlErrorNoActiveQuestion {
							t.Error("accepted after cancellation", code)
						}
						return
					}
					if code := r.acceptQuestionAnswer(e.TurnID, questionResponse(t, p, "  exact answer\n")); code != "" {
						t.Error("rejected answer", code)
						cancel()
						return
					}
					if code := r.acceptQuestionAnswer(e.TurnID, questionResponse(t, p, "duplicate")); code != protocol.ControlErrorQuestionMismatch {
						t.Error("accepted duplicate", code)
					}
					if mode == "cancel_after_answer" {
						cancel()
					}
				case protocol.EventUserQuestionResolved:
					resolved++
					found := false
					for _, batch := range journal.snapshot() {
						for _, draft := range batch {
							found = found || draft.EventKind() == session.EventToolCallResult
						}
					}
					if !found {
						t.Error("resolved before Sync")
					}
				}
			})
			if mode == "sync_failure" {
				if err == nil || resolved != 0 || !r.poisoned.Load() {
					t.Fatal("Sync failure claimed completion")
				}
				return
			}
			if mode == "unavailable" {
				if requested != 0 || resolved != 0 || len(c.results) != 1 || c.results[0].Code() != "user_input_unavailable" {
					t.Fatal("headless question waited or lost output")
				}
				return
			}
			if requested != 1 || resolved != 1 {
				t.Fatalf("requested/resolved=%d/%d err=%v", requested, resolved, err)
			}
			if mode == "cancel_before_answer" {
				if c.results[0].Status() != tool.ResultCancelled {
					t.Fatal("invented answer")
				}
			} else {
				a, err := c.results[0].QuestionAnswers()
				if err != nil || a.Answers()[0].Text != "  exact answer\n" {
					t.Fatal("accepted answer was lost", err)
				}
			}
			if r.questions.active != nil {
				t.Fatal("pending question leaked")
			}
			if r.approvals.patchGrant.Load() || r.approvals.writeGrant.Load() {
				t.Fatal("answer granted permissions")
			}
		})
	}
}
func TestQuestionRejectsOversizedAnswerWithoutChangingLedger(t *testing.T) {
	journal := &fakeJournal{}
	r, _ := runtimeQuestionFixture(t, journal)
	r.resultBudget = tool.NewResultBudget()
	if err := r.resultBudget.BeginSample(3); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		allowance, _ := r.resultBudget.Allowance(tool.CapabilityRead)
		m, _ := tool.NewResultBudgetMetadata(allowance, allowance, tool.BudgetPreserved)
		if err := r.resultBudget.Commit(tool.CapabilityRead, m); err != nil {
			t.Fatal(err)
		}
	}
	input, _ := tool.NewQuestionInput([]tool.Question{{ID: "choice", Header: "Choice", Question: "Choose?", Options: []tool.QuestionOption{{Label: "A", Description: "a"}, {Label: "B", Description: "b"}}}})
	call, _ := tool.NewQuestionReadyCall("question-call", input)
	inv, _ := tool.NewInvocation("01900000-0000-7000-8000-000000000002", call)
	before := r.resultBudget.SampleUsed()
	result, _, err := r.executeExclusiveCall(context.Background(), func(e protocol.Event) {
		if e.Kind != protocol.EventUserQuestionRequested {
			return
		}
		p, _ := protocol.DecodeUserQuestionRequested(e)
		if p.ResultAllowance != 1024 {
			t.Error("incorrect allowance", p.ResultAllowance)
		}
		if code := r.acceptQuestionAnswer(e.TurnID, questionResponse(t, p, strings.Repeat("\x01", 500))); code != protocol.ControlErrorQuestionAnswerInvalid {
			t.Error("accepted escaping expansion", code)
		}
		if r.resultBudget.SampleUsed() != before {
			t.Error("rejected answer charged budget")
		}
		if code := r.acceptQuestionAnswer(e.TurnID, questionResponse(t, p, "short")); code != "" {
			t.Error("could not retry edited answer", code)
		}
	}, runtimeTurnID, inv)
	if err != nil || result.Status() != tool.ResultSuccess || result.Budget().Allowance() != 1024 {
		t.Fatal("question failed", err)
	}
}
