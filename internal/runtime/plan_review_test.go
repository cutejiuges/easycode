package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

const planReviewFixture = "# 方案\r\n\tKeep /skill and @README.md literal.\n"

type planReviewConversation struct {
	*controlledConversation
	encoding func() error
}

func (*planReviewConversation) ProjectPlanProposal(domain.AssistantSampleReference) (string, bool) {
	return planReviewFixture, true
}

func (c *planReviewConversation) EncodeUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	if c.encoding != nil && inputs[0].Text() == domain.PlanImplementationInput(planReviewFixture) {
		if err := c.encoding(); err != nil {
			return provider.NativeCommitEnvelope{}, err
		}
	}
	return c.controlledConversation.EncodeUserInputs(a, inputs)
}

func receivePlanControl(t *testing.T, ctx context.Context, host *ChatSession) protocol.ControlItem {
	t.Helper()
	select {
	case item, open := <-host.Outputs():
		if !open {
			t.Fatal("control stream closed before expected item")
		}
		return item
	case <-ctx.Done():
		t.Fatal("expected control item did not arrive")
		return protocol.ControlItem{}
	}
}

func openPlanReviewFixture(t *testing.T, ctx context.Context, r *Runtime) *ChatSession {
	t.Helper()
	host := NewChatSession(r)
	if err := host.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	if result, err := host.SetWorkMode(ctx, "plan", domain.WorkModePlan); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatalf("cannot enter Plan: %v, %v", result, err)
	}
	return host
}

func TestPlanApprovalStartsOneLiteralInput(t *testing.T) {
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	entered, release := make(chan struct{}), make(chan struct{})
	var encodings atomic.Int32
	conversation := &planReviewConversation{controlledConversation: newControlledConversation(), encoding: func() error {
		switch encodings.Add(1) {
		case 1:
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		case 2:
			return errors.New("fixture encoding limit")
		}
		return nil
	}}
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	host := openPlanReviewFixture(t, ctx, loop.runtime)
	text, _ := domain.NewLiteralInputDraft("Plan the work")
	initial, err := host.Append(ctx, text)
	if err != nil || !initial.EntryID().Valid() {
		t.Fatalf("cannot start Plan: %v, %v", initial, err)
	}
	receiveInvocation(t, conversation.calls).release <- completedProviderEvent(t)

	var offer protocol.PlanReviewOffer
	completed := false
	for !offer.Ref.Sample.TurnID.Valid() {
		item := receivePlanControl(t, ctx, host)
		if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventTurnCompleted {
			completed = true
		}
		if candidate, ok := item.PlanReviewOffer(); ok {
			offer = candidate
		}
	}
	if !completed || offer.Markdown != planReviewFixture {
		t.Fatal("candidate did not follow its durable terminal")
	}
	command, _ := protocol.NewApprovePlanCommand("headless", offer.Ref)
	if _, err := host.loop.Submit(ctx, command); fault.Project(err).Code != fault.CodeInvalidInput {
		t.Fatalf("noninteractive approval reached owner: %v", err)
	}
	permit, err := host.ReserveHandoff(ctx, HandoffResume)
	requireNoError(t, err)
	if _, err := host.ApprovePlan(ctx, "during-resume", offer.Ref); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("approval crossed resume reservation: %v", err)
	}
	if err := permit.Release(ctx); err != nil {
		t.Fatal(err)
	}

	before, cancelBefore := context.WithCancel(ctx)
	defer cancelBefore()
	rejected := make(chan error, 1)
	go func() {
		_, err := host.ApprovePlan(before, "before", offer.Ref)
		rejected <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("approval did not arrive at encoding preflight")
	}
	if _, err := host.Append(ctx, text); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("input crossed pending confirmation: %v", err)
	}
	cancelBefore()
	close(release)
	if err := <-rejected; fault.Project(err).Code != fault.CodeUserCancelled {
		t.Fatalf("cancelled delivery consumed the proposal: %v", err)
	}
	result, err := host.ApprovePlan(ctx, "encoding-limit", offer.Ref)
	requireNoError(t, err)
	assertRejectedCode(t, result, protocol.ControlErrorInputEncodingLimit)
	stale := offer.Ref
	stale.Sample.SampleIndex++
	result, err = host.ApprovePlan(ctx, "stale", stale)
	requireNoError(t, err)
	assertRejectedCode(t, result, protocol.ControlErrorPlanUnavailable)
	high, _ := domain.NewExplicitEffortChoice("high")
	if _, err := host.SetEffort(ctx, "effort", high); err != nil {
		t.Fatal(err)
	}
	after, cancelAfter := context.WithCancel(ctx)
	result, err = host.ApprovePlan(after, "implement", offer.Ref)
	cancelAfter()
	if err != nil || !result.EntryID().Valid() {
		t.Fatalf("approval failed: %v, %v", result, err)
	}
	late, err := host.Append(ctx, text)
	if err != nil || late.Disposition() != protocol.CommandDispositionQueued {
		t.Fatalf("later input crossed approved start: %v, %v", late, err)
	}
	invocation := receiveInvocation(t, conversation.calls)
	if invocation.ctx.Err() != nil || invocation.input != domain.PlanImplementationInput(planReviewFixture) ||
		invocation.settings.OpenAIEffort() != "high" || len(invocation.history.Turns[1].Messages) != 1 {
		t.Fatalf("implementation lost literal content, sole ownership, or selected effort: %+v", invocation)
	}
	acknowledged, confirmed := false, false
	for {
		item := receivePlanControl(t, ctx, host)
		if item.RequestID() != "implement" {
			continue
		}
		if ack, ok := item.CommandResult(); ok {
			acknowledged = ack == result
		}
		if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventWorkModeSnapshot {
			mode, _ := protocol.DecodeWorkModeSnapshot(event)
			confirmed = acknowledged && mode.Mode == domain.WorkModeDefault
		}
		if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventTurnStarted && !confirmed {
			t.Fatal("implementation started before its Default confirmation")
		}
		if status, ok := item.InputStatus(); ok && status.State() == protocol.InputCommitted {
			if !confirmed || status.EntryID() != result.EntryID() {
				t.Fatal("durable implementation input lost its admitted identity")
			}
			break
		}
	}
	duplicate, err := host.ApprovePlan(ctx, "implement", offer.Ref)
	requireNoError(t, err)
	assertRejectedCode(t, duplicate, protocol.ControlErrorDuplicateRequest)
}

func TestPlanReviewRetiresCancelledOrSupersededSample(t *testing.T) {
	for _, reason := range []string{"interrupt", "queued correction"} {
		t.Run(reason, func(t *testing.T) {
			loop := newControlledAgentLoop(t, &planReviewConversation{controlledConversation: newControlledConversation()}, &fakeJournal{})
			loop.interactiveStopped = make(chan struct{})
			state := newAgentLoopRunState(loop, context.Background())
			plan, err := state.selectedProfile.WithWorkMode(domain.WorkModePlan)
			requireNoError(t, err)
			state.selectedProfile = plan
			state.active = &activeLoopTurn{
				profile: plan, input: queuedInput{requestID: "planning"}, turnID: runtimeTurnID,
				worker: loopWorker{cancel: func() {}},
			}
			commit, _ := protocol.NewSampleTranscriptCommitted(3, 0, domain.EmptySamplePresentation(1))
			completed, _ := protocol.NewTurnCompleted(testRuntimeUsage(t))
			for _, event := range []protocol.Event{commit, completed} {
				state.handleTurnEvent(runtimePublication{event: loop.runtime.decorate(event, runtimeTurnID)}, true)
				<-loop.outputs
			}
			if reason == "interrupt" {
				command, _ := protocol.NewInterruptCommand("interrupt", runtimeTurnID)
				request := loopCommandRequest{command: command, result: make(chan loopCommandResponse, 1)}
				state.handleCommand(request)
				if reply := <-request.result; reply.result.Status() != protocol.CommandStatusAccepted {
					t.Fatalf("owner did not accept the late interrupt: %+v", reply)
				}
				<-loop.outputs
			} else {
				id, _ := domain.GenerateInputID()
				input, _ := domain.NewUserInput(id, "new constraint")
				state.enqueueInput(queuedInput{value: input, automatic: true, requestID: "correction"})
			}
			state.handleTurnFinished(nil, true)
			if len(loop.outputs) != 0 || state.planReview.Markdown != "" {
				t.Fatal("owner exposed a cancelled or superseded proposal")
			}
		})
	}
}

func TestPlanApprovalOwnsUncertainInputWithoutRetry(t *testing.T) {
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	// 首轮 3 个批次，实施回合的第 2 个批次提交新输入。
	journal := &fakeJournal{failAt: 5}
	conversation := &planReviewConversation{controlledConversation: newControlledConversation()}
	runtime := newControlledAgentLoop(t, conversation, journal).runtime
	host := openPlanReviewFixture(t, ctx, runtime)
	draft, _ := domain.NewLiteralInputDraft("Plan")
	if _, err := host.Append(ctx, draft); err != nil {
		t.Fatal(err)
	}
	receiveInvocation(t, conversation.calls).release <- completedProviderEvent(t)
	for {
		if candidate, ok := receivePlanControl(t, ctx, host).PlanReviewOffer(); ok {
			result, err := host.ApprovePlan(ctx, "implement", candidate.Ref)
			if err != nil || !result.EntryID().Valid() {
				t.Fatalf("approval failed before the fault: %v, %v", result, err)
			}
			uncertain := false
			for item := range host.Outputs() {
				if status, ok := item.InputStatus(); ok && status.EntryID() == result.EntryID() {
					if status.State() == protocol.InputCommitted || status.State() == protocol.InputReturned {
						t.Fatal("uncertain input was exposed as a definite result")
					}
					uncertain = uncertain || status.State() == protocol.InputUncertain
				}
			}
			if !uncertain || !runtime.poisoned.Load() || len(conversation.calls) != 0 {
				t.Fatal("uncertain input did not poison the session or was sent to the provider")
			}
			return
		}
	}
}
