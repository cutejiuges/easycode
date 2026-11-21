package runtime

import (
	"context"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestWorkModeRejectsBusyAndClosedOwner(t *testing.T) {
	journal := &fakeJournal{}
	loop := newControlledAgentLoop(t, newControlledConversation(), journal)
	closed := make(chan struct{})
	close(closed)
	for _, condition := range []string{"finishing", "compacting", "queued", "handoff", "caller", "host", "runtime", "poisoned"} {
		t.Run(condition, func(t *testing.T) {
			state := newAgentLoopRunState(loop, context.Background())
			command, _ := protocol.NewSetWorkModeCommand("switch", domain.WorkModePlan)
			request := loopCommandRequest{command: command, result: make(chan loopCommandResponse, 1)}
			want := protocol.ControlErrorSessionBusy
			switch condition {
			case "finishing":
				state.active = &activeLoopTurn{turnID: runtimeTurnID, terminalSeen: true}
			case "compacting":
				state.compaction = &activeLoopCompaction{}
			case "queued":
				state.queue = append(state.queue, queuedInput{})
			case "handoff":
				state.resumePermit = &ResumePermit{}
			case "caller":
				request.cancelled = closed
			case "host":
				request.hostStopped = closed
				want = protocol.ControlErrorSessionClosing
			case "runtime":
				state.runContextDone = closed
				want = protocol.ControlErrorSessionClosing
			case "poisoned":
				journal.poisoned = true
				want = protocol.ControlErrorSessionFailed
			}
			state.handleCommand(request)
			reply := <-request.result
			if condition == "caller" {
				if fault.Project(reply.err).Code != fault.CodeUserCancelled {
					t.Fatalf("cancelled transition: %+v", reply)
				}
			} else {
				assertRejectedCode(t, reply.result, want)
				if len(loop.outputs) != 1 {
					t.Fatalf("rejected transition produced %d outputs", len(loop.outputs))
				}
				<-loop.outputs
			}
			if len(loop.outputs) != 0 || state.selectedProfile.WorkMode() != domain.WorkModeDefault || len(journal.snapshot()) != 0 {
				t.Fatal("rejected transition confirmed a change or altered the owner or journal")
			}
		})
	}
}

func TestWorkModeHostReservationAndAcceptedCancellation(t *testing.T) {
	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	journal := &fakeJournal{}
	loop := newControlledAgentLoop(t, newControlledConversation(), journal)
	host := NewChatSession(loop.runtime)
	host.loop, host.started, host.interactive = loop, true, true
	loop.state.Store(agentLoopRunning)
	state := newAgentLoopRunState(loop, ctx)
	command, _ := protocol.NewSetWorkModeCommand("direct-work-mode", domain.WorkModePlan)
	if _, err := loop.Submit(ctx, command); fault.Project(err).Code != fault.CodeInvalidInput {
		t.Fatalf("generic host reached mode control: %v", err)
	}

	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	replies := make(chan loopCommandResponse, 1)
	go func() {
		result, err := host.SetWorkMode(caller, "plan", domain.WorkModePlan)
		replies <- loopCommandResponse{result: result, err: err}
	}()
	var request loopCommandRequest
	select {
	case request = <-loop.requests:
	case <-ctx.Done():
		t.Fatal("mode request did not reach the owner")
	}
	draft, _ := domain.NewInteractiveInputDraft("Draft @README.md")
	if _, err := host.Append(ctx, draft); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("preparation crossed the mode reservation: %v", err)
	}
	if _, err := host.ReserveResume(ctx); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("resume crossed the mode reservation: %v", err)
	}
	state.handleCommand(request)
	cancel()
	reply := <-replies
	if reply.err != nil || reply.result.Disposition() != protocol.CommandDispositionUpdated {
		t.Fatalf("accepted transition lost its result: %+v", reply)
	}
	ack, ok := (<-loop.outputs).CommandResult()
	if !ok || ack != reply.result {
		t.Fatal("mode confirmation was not preceded by its accepted result")
	}
	item := <-loop.outputs
	event, ok := item.ContextEvent()
	payload, err := protocol.DecodeWorkModeSnapshot(event)
	if !ok || err != nil || item.RequestID() != "plan" || event.SessionID != runtimeSessionID || event.ThreadID != runtimeThreadID || payload.Mode != domain.WorkModePlan || len(journal.snapshot()) != 0 {
		t.Fatalf("expected correlated process-only mode change: %v, %v, %v", item.RequestID(), payload, err)
	}
	if err := host.beginAppend(); err != nil {
		t.Fatalf("accepted transition retained its reservation: %v", err)
	}
	host.finishAppend()

	state.handleCommand(request)
	assertRejectedCode(t, (<-request.result).result, protocol.ControlErrorDuplicateRequest)
}

func TestWorkModeAndEffortAreIndependentSelections(t *testing.T) {
	loop := newControlledAgentLoop(t, newControlledConversation(), &fakeJournal{})
	state := newAgentLoopRunState(loop, context.Background())
	run := func(command protocol.Command) {
		t.Helper()
		request := loopCommandRequest{command: command, result: make(chan loopCommandResponse, 1)}
		state.handleCommand(request)
		reply := <-request.result
		if reply.err != nil || reply.result.Status() != protocol.CommandStatusAccepted {
			t.Fatalf("command was not accepted: %+v", reply)
		}
		<-loop.outputs
		<-loop.outputs
	}
	high, _ := domain.NewExplicitEffortChoice("high")
	chooseEffort, _ := protocol.NewSetEffortCommand("high", high)
	enter, _ := protocol.NewSetWorkModeCommand("enter", domain.WorkModePlan)
	reset, _ := protocol.NewSetEffortCommand("reset", domain.NewResetEffortChoice())
	exit, _ := protocol.NewSetWorkModeCommand("exit", domain.WorkModeDefault)
	run(chooseEffort)
	run(enter)
	state.active = &activeLoopTurn{profile: state.selectedProfile, input: queuedInput{requestID: "turn"}, selectionRevision: state.selectionRevision}
	run(reset)
	if state.active.profile.WorkMode() != domain.WorkModePlan || state.active.profile.InferenceSettings().OpenAIEffort() != "high" ||
		state.selectedProfile.WorkMode() != domain.WorkModePlan || state.selectedProfile.InferenceSettings() != loop.runtime.profile.InferenceSettings() {
		t.Fatal("effort reset altered active settings or selected plan mode")
	}
	state.active = nil
	inPlanHigh, _ := protocol.NewSetEffortCommand("plan-high", high)
	run(inPlanHigh)
	run(exit)
	input, err := loop.runtime.samplingInputFor(state.selectedProfile)
	if err != nil || input.WorkMode() != domain.WorkModeDefault || input.InferenceSettings().OpenAIEffort() != "high" {
		t.Fatalf("mode exit lost chosen effort: %v, %v", input, err)
	}
}

func TestPlanCompactionSeparatesSummaryFromOrdinaryWindowPolicy(t *testing.T) {
	value := newTestRuntime(t, newCompactionConversation(), &fakeJournal{})
	planProfile, err := value.profile.WithWorkMode(domain.WorkModePlan)
	if err != nil {
		t.Fatal(err)
	}
	admission, rejection := value.preflightCompactionFor(planProfile, "Preserve decisions")
	if rejection != "" {
		t.Fatal(rejection)
	}
	summary := admission.preview.Input().Sampling()
	summaryTools, present, err := summary.ToolCatalog()
	if err != nil || !present || summary.WorkMode() != domain.WorkModeDefault || summaryTools.Fingerprint() != value.toolCatalog.Fingerprint() {
		t.Fatal("summary preflight did not use the full summary policy")
	}
	ordinary, err := value.contextPlanFor(planProfile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ordinary.CachePlan().StablePrefixFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	got, err := admission.before.CachePlan().StablePrefixFingerprint()
	if err != nil || got != want {
		t.Fatalf("ordinary window preflight lost the plan policy: %v", err)
	}
}

func TestPlanModeRejectsPreparedMutationBeforeAnyCallIsRecorded(t *testing.T) {
	read := testRuntimeReadyCall(t, "call-read")
	writeInput, _ := tool.NewWriteInput("new-file", "content")
	write, _ := tool.NewWriteReadyCall("call-write", writeInput)
	sample := newPreparedSampleWithReady(t, read, write)
	executor := &recordingReadExecutor{}
	journal := &fakeJournal{}
	config := testRuntimeConfig(t, journal)
	config.ToolBinding = testRuntimeToolBinding(t, func(components *tool.Components) { components.Read = executor })
	conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
		stream: fixedStream(completedProviderEventWithSample(t, sample)),
	}}
	value, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	value.approvals.writeGrant.Store(true)
	profile, err := value.profile.WithWorkMode(domain.WorkModePlan)
	if err != nil {
		t.Fatal(err)
	}
	var events []protocol.Event
	err = value.runTurn(context.Background(), runtimeInputs("Plan the change"), profile, nil, func(event protocol.Event) {
		events = append(events, event)
	})
	if fault.Project(err).Code != fault.CodeStreamProtocol {
		t.Fatalf("prepared mutation failed with unexpected error: %v", err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
	if _, err := sample.Envelope(); err == nil || sample.Finalized() {
		t.Fatal("rejected sample staging was not discarded")
	}
	batches := journal.snapshot()
	if executor.calls.Load() != 0 || conversation.prepareCalls.Load() != 0 || len(batches) != 3 ||
		len(batches[1]) != 2 || batches[1][0].EventKind() != session.EventTurnInputsCommitted ||
		len(batches[2]) != 1 || batches[2][0].EventKind() != session.EventTurnFailed {
		t.Fatalf("rejected sample had side effects: reads=%d outputs=%d batches=%+v", executor.calls.Load(), conversation.prepareCalls.Load(), batches)
	}
}
