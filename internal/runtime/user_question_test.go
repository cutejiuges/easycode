package runtime

import (
	"context"
	"errors"
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
	draft, _ := domain.NewInteractiveInputDraft("ask")
	if _, err := host.Append(ctx, draft); err != nil {
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
	config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Question = builtin.NewQuestionExecutor() })
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
