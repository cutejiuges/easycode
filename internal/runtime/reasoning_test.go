package runtime

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"errors"
	"fmt"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func preparedReasoning(t *testing.T, readable bool, ready ...tool.ReadyCall) *provider.PreparedSample {
	t.Helper()
	base := newPreparedSample(t, func() {})
	envelope, _ := base.Envelope()
	usage, _ := base.Usage()
	receipt, _ := base.Receipt()
	availability := domain.ReasoningUnavailable
	if readable {
		availability = domain.ReasoningReadable
	}
	presentation, err := domain.NewSamplePresentation(uint32(1+len(ready)), []domain.ReasoningBinding{{Block: 0, Item: 0, Source: domain.ReasoningOpenAISummary, Availability: availability}})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := provider.NewPreparedSample(envelope, usage, receipt, presentation, func() {}, func() {}, ready...)
	if err != nil {
		t.Fatal(err)
	}
	return sample
}
func reasoningStreamEvents(t *testing.T, complete bool) []provider.StreamEvent {
	t.Helper()
	start, _ := protocol.NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	delta, _ := protocol.NewReasoningDelta(0, domain.ReasoningOpenAISummary, 0, "allowed")
	events := []provider.StreamEvent{semanticProviderEvent(t, start), semanticProviderEvent(t, delta)}
	if complete {
		closed, _ := protocol.NewReasoningCompleted(0, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: domain.ReasoningReadable, TotalBytes: 7, Text: "allowed"})
		events = append(events, semanticProviderEvent(t, closed), completedProviderEventWithSample(t, preparedReasoning(t, true)))
	} else {
		failed, _ := provider.NewFailedStreamEvent(errors.New("fixture stream failed"))
		events = append(events, failed)
	}
	return events
}
func TestReasoningRuntimeScopeAndCommitWithoutQueryConsumer(t *testing.T) {
	journal := &fakeJournal{}
	r := newTestRuntime(t, &fakeConversation{stream: fixedStream(reasoningStreamEvents(t, true)...)}, journal)
	events, err := collectTurn(r, context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	var state protocol.ReasoningStreamState
	for _, event := range events {
		switch event.Kind {
		case protocol.EventAssistantReasoning:
			p, err := protocol.DecodeAssistantReasoning(event)
			if err != nil {
				t.Fatal(err)
			}
			if event.TurnID != runtimeTurnID || p.SampleIndex != 0 {
				t.Fatal("scope not bound")
			}
			if err := state.Accept(p); err != nil {
				t.Fatal(err)
			}
		case protocol.EventTranscriptCommitted:
			p, err := protocol.DecodeTranscriptCommitted(event)
			if err != nil {
				t.Fatal(err)
			}
			if p.Origin == protocol.TranscriptCommitSample {
				commits++
				if p.Sequence == 0 || p.Sample.Bindings[0].Item != 0 {
					t.Fatal(p)
				}
				if err := state.Commit(*p.Sample); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if commits != 1 || state.Pending() {
		t.Fatal("missing exact sample commit")
	}
}
func TestReasoningRuntimeRejectsBindingBeforeJournalAdmission(t *testing.T) {
	events := reasoningStreamEvents(t, true)
	events[len(events)-1] = completedProviderEvent(t)
	journal := &fakeJournal{}
	r := newTestRuntime(t, &fakeConversation{stream: fixedStream(events...)}, journal)
	output, err := collectTurn(r, context.Background(), nil)
	if err == nil {
		t.Fatal("mismatch accepted")
	}
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventSampleUsage {
				t.Fatal("mismatched sample reached journal")
			}
		}
	}
	p, err := protocol.DecodeTurnFailed(output[len(output)-1])
	if err != nil || p.Resolution == nil || p.Resolution.Disposition != protocol.SampleNotCommitted {
		t.Fatal(p, err)
	}
}
func TestReasoningFailureResolutionTracksActualDurability(t *testing.T) {
	for _, mode := range []string{"provider", "sync", "projection"} {
		t.Run(mode, func(t *testing.T) {
			journal := &fakeJournal{}
			stream := reasoningStreamEvents(t, mode != "provider")
			r := newTestRuntime(t, &fakeConversation{stream: fixedStream(stream...)}, journal)
			want := protocol.SampleNotCommitted
			if mode == "sync" {
				journal.failAt = 3
				want = protocol.SampleUncertain
			}
			if mode == "projection" {
				r.toolHistory = &reasoningProjectionFailure{}
				want = protocol.SampleDurable
			}
			events, err := collectTurn(r, context.Background(), nil)
			if err == nil {
				t.Fatal("failure missing")
			}
			p, err := protocol.DecodeTurnFailed(events[len(events)-1])
			if err != nil || p.Resolution == nil || p.Resolution.Disposition != want {
				t.Fatal(p, err)
			}
			if want == protocol.SampleDurable && p.Resolution.Committed.Sequence == 0 {
				t.Fatal("durable location missing")
			}
		})
	}
}

type reasoningProjectionFailure struct{ calls int }

func (observer *reasoningProjectionFailure) NativeCommitted(uint64, []session.ToolHistoryEntry) error {
	observer.calls++
	if observer.calls == 1 {
		return nil
	}
	return errors.New("fixture projection failed")
}
func (*reasoningProjectionFailure) ToolResult(tool.InvocationResult) {}

func TestReasoningCancellationValidatesBoundariesAndSuppressesDelta(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := reasoningStreamEvents(t, true)
	r := newTestRuntime(t, &fakeConversation{stream: fixedStream(events...)}, &fakeJournal{})
	output, err := collectTurn(r, ctx, func(event protocol.Event) {
		if event.Kind == protocol.EventAssistantReasoning {
			p, _ := protocol.DecodeAssistantReasoning(event)
			if p.Stage == protocol.ReasoningStarted {
				cancel()
			}
		}
	})
	if err != nil {
		t.Fatal("selected completed should commit", err)
	}
	stages := []protocol.ReasoningStage{}
	for _, event := range output {
		if event.Kind == protocol.EventAssistantReasoning {
			p, _ := protocol.DecodeAssistantReasoning(event)
			stages = append(stages, p.Stage)
		}
	}
	if len(stages) != 2 || stages[0] != protocol.ReasoningStarted || stages[1] != protocol.ReasoningCompleted {
		t.Fatal(stages)
	}
}
func TestReasoningTransientPublicationDoesNotReplan(t *testing.T) {
	for _, count := range []int{1, 10000} {
		r := newTestRuntime(t, &fakeConversation{}, &fakeJournal{})
		calls := 0
		r.planner = contextPlannerFunc(func(input contextplan.PlanningInput) (contextplan.ContextPlan, error) {
			calls++
			return contextplan.NewPlanner().Plan(input)
		})
		before, _ := r.contextPlan()
		_ = before
		baseline := calls
		for i := range count {
			event, _ := protocol.NewReasoningDelta(0, domain.ReasoningOpenAISummary, uint64(i), "x")
			publication := r.publishContext(event)
			if publication.snapshotErr != nil || publication.event.Kind != protocol.EventAssistantReasoning {
				t.Fatal("transient publication changed")
			}
		}
		if calls != baseline {
			t.Fatal("delta replanned", count, calls, baseline)
		}
	}
	if protocol.IsTransientPresentation(protocol.EventTranscriptCommitted) || !protocol.IsTransientPresentation(protocol.EventAssistantReasoning) {
		t.Fatal("wrong classification")
	}
}

func TestReasoningForwardingHasOnlyTwoBufferedBoundaries(t *testing.T) {
	r := newTestRuntime(t, &fakeConversation{}, &fakeJournal{})
	host := NewChatSession(r)
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	if cap(host.outputs) != 0 || cap(loop.outputs) != 16 {
		t.Fatal("host adapter introduced another payload queue")
	}
	// Provider 的 16 项容量由两家真实 stream 集成测试及 PTY 消费；宿主退出排空沿现有确定性门闩回归。
	if 2*16*protocol.MaxReasoningPayloadBytes > 16<<20 {
		t.Fatal("queue payload budget exceeds contract")
	}
}

type reasoningCountingConversation struct {
	fakeConversation
	projections, footprints int
}

func (c *reasoningCountingConversation) ProjectHistory() domain.SemanticHistoryView {
	c.projections++
	return c.fakeConversation.ProjectHistory()
}
func (c *reasoningCountingConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	c.footprints++
	return c.fakeConversation.HistoryFootprint()
}

func TestReasoningProducerAndConsumerReuseCommittedSnapshots(t *testing.T) {
	for _, count := range []int{1, 10000} {
		conversation := &reasoningCountingConversation{}
		r := newTestRuntime(t, conversation, &fakeJournal{})
		calls := 0
		r.planner = contextPlannerFunc(func(input contextplan.PlanningInput) (contextplan.ContextPlan, error) {
			calls++
			return contextplan.NewPlanner().Plan(input)
		})
		loop, _ := NewAgentLoop(r, DefaultAgentLoopConfig())
		state := newAgentLoopRunState(loop, context.Background())
		state.active = &activeLoopTurn{input: queuedInput{requestID: "reasoning-count"}, turnID: runtimeTurnID, cancel: func() {}}
		state.contextSnapshot, state.contextSnapshotErr = r.contextSnapshot()
		state.cacheSnapshot = r.cache.snapshot
		contextBefore, _ := codec.MarshalStable(state.contextSnapshot)
		cacheBefore, _ := codec.MarshalStable(state.cacheSnapshot)
		planBefore, projectBefore, footprintBefore := calls, conversation.projections, conversation.footprints
		for i := range count {
			event, _ := protocol.NewReasoningDelta(0, domain.ReasoningOpenAISummary, uint64(i), "x")
			event = r.decorate(event, runtimeTurnID)
			state.handleTurnEvent(r.publishContext(event), true)
			item := <-loop.outputs
			if _, ok := item.TurnEvent(); !ok {
				t.Fatal("delta not forwarded")
			}
		}
		contextAfter, _ := codec.MarshalStable(state.contextSnapshot)
		cacheAfter, _ := codec.MarshalStable(state.cacheSnapshot)
		if calls != planBefore || conversation.projections != projectBefore || conversation.footprints != footprintBefore || !bytes.Equal(contextBefore, contextAfter) || !bytes.Equal(cacheBefore, cacheAfter) {
			t.Fatal("transient event recomputed or replaced committed snapshot", count)
		}
	}
}

func TestReasoningControlTransitionsPrecedeFinalOutput(t *testing.T) {
	r := newTestRuntime(t, &fakeConversation{}, &fakeJournal{})
	loop, _ := NewAgentLoop(r, DefaultAgentLoopConfig())
	state := newAgentLoopRunState(loop, context.Background())
	cancelled := false
	state.active = &activeLoopTurn{input: queuedInput{requestID: "active-reasoning"}, turnID: runtimeTurnID, cancel: func() { cancelled = true }}
	event, _ := protocol.NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	event = r.decorate(event, runtimeTurnID)
	state.handleTurnEvent(r.publishContext(event), true)
	<-loop.outputs
	submit, _ := protocol.NewSubmitConversationalInputCommand("during-reasoning", "queued input")
	slot := loopCommandRequest{command: submit, result: make(chan loopCommandResponse, 1)}
	state.handleCommand(slot)
	response := <-slot.result
	if response.err != nil || len(state.queue) != 1 || state.active.terminalSeen {
		t.Fatal("input not accepted during reasoning", response.err)
	}
	interrupt, _ := protocol.NewInterruptCommand("stop-reasoning", runtimeTurnID)
	slot = loopCommandRequest{command: interrupt, result: make(chan loopCommandResponse, 1)}
	state.handleCommand(slot)
	response = <-slot.result
	if response.err != nil || !cancelled || state.active.terminalSeen {
		t.Fatal("interrupt was deferred until final output")
	}
	shutdown, _ := protocol.NewShutdownCommand("close-reasoning")
	slot = loopCommandRequest{command: shutdown, result: make(chan loopCommandResponse, 1)}
	state.handleCommand(slot)
	response = <-slot.result
	if response.err != nil || state.mode != loopModeClosing || state.active.terminalSeen {
		t.Fatal("shutdown was deferred until final output")
	}
}

type reasoningAdmissionJournal struct {
	fakeJournal
	admitted, release      chan struct{}
	sampleContextCancelled bool
}

func (j *reasoningAdmissionJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	for _, draft := range drafts {
		if draft.EventKind() == session.EventSampleUsage {
			close(j.admitted)
			<-j.release
			j.sampleContextCancelled = ctx.Err() != nil
			break
		}
	}
	return j.fakeJournal.AppendBatch(ctx, drafts)
}
func TestReasoningCancellationDuringAcceptedCommitWaitsForRealResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal := &reasoningAdmissionJournal{admitted: make(chan struct{}), release: make(chan struct{})}
			if fail {
				journal.failAt = 3
			}
			r := newTestRuntime(t, &fakeConversation{stream: fixedStream(reasoningStreamEvents(t, true)...)}, journal)
			type result struct {
				events []protocol.Event
				err    error
			}
			done := make(chan result, 1)
			go func() { events, err := collectTurn(r, ctx, nil); done <- result{events, err} }()
			<-journal.admitted
			cancel()
			select {
			case <-done:
				t.Fatal("cancel abandoned accepted commit owner")
			default:
			}
			close(journal.release)
			output := <-done
			if journal.sampleContextCancelled {
				t.Fatal("accepted sample inherited cancellation")
			}
			if fail {
				p, err := protocol.DecodeTurnFailed(output.events[len(output.events)-1])
				if err != nil || p.Resolution == nil || p.Resolution.Disposition != protocol.SampleUncertain {
					t.Fatal(p, err)
				}
			} else {
				if output.err != nil || output.events[len(output.events)-1].Kind != protocol.EventTurnCompleted {
					t.Fatal(output.err)
				}
				commits := 0
				for _, event := range output.events {
					if event.Kind == protocol.EventTranscriptCommitted {
						p, _ := protocol.DecodeTranscriptCommitted(event)
						if p.Origin == protocol.TranscriptCommitSample {
							commits++
						}
					}
				}
				if commits != 1 {
					t.Fatal("accepted durable commit was not emitted exactly once")
				}
			}
		})
	}
}

func TestReasoningMultipleSamplesAndToolCancellationDoNotRelabelPriorCommit(t *testing.T) {
	for _, cancelTools := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelTools), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := reasoningStreamEvents(t, true)
			first[len(first)-1] = completedProviderEventWithSample(t, preparedReasoning(t, true, testRuntimeReadyCall(t, "call-reasoning")))
			sampleIndex := 0
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{stream: func(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
				sampleIndex++
				if sampleIndex == 1 {
					return fixedStream(first...)(ctx, input)
				}
				return fixedStream(reasoningStreamEvents(t, true)...)(ctx, input)
			}}}
			executor := &recordingReadExecutor{}
			config := testRuntimeConfig(t, &fakeJournal{})
			config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
			config.ReadExecutor = executor
			config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
			r, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			events, runErr := collectTurn(r, ctx, func(event protocol.Event) {
				if cancelTools && event.Kind == protocol.EventTranscriptCommitted {
					p, _ := protocol.DecodeTranscriptCommitted(event)
					if p.Origin == protocol.TranscriptCommitSample {
						cancel()
					}
				}
			})
			commits := 0
			lastIndex := uint32(0)
			for _, event := range events {
				if event.Kind == protocol.EventTranscriptCommitted {
					p, _ := protocol.DecodeTranscriptCommitted(event)
					if p.Origin == protocol.TranscriptCommitSample {
						if p.Sample.SampleIndex != uint32(commits) {
							t.Fatal("real sample index was not bound")
						}
						lastIndex = p.Sample.SampleIndex
						commits++
					}
				}
			}
			if cancelTools {
				failure, err := protocol.DecodeTurnFailed(events[len(events)-1])
				if err != nil || failure.Resolution != nil || commits != 1 || executor.calls.Load() != 0 {
					t.Fatal("tool cancellation relabeled prior sample", failure, err, commits)
				}
			} else if runErr != nil || commits != 2 || lastIndex != 1 || executor.calls.Load() != 1 {
				t.Fatal("multiple samples failed", runErr, commits, lastIndex)
			}
		})
	}
}
