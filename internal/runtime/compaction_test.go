package runtime

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type compactionConversation struct {
	*fakeConversation
	generated       atomic.Int32
	installed       atomic.Bool
	discarded       atomic.Int32
	candidateTokens uint64
	generationErr   error
	entered         chan struct{}
	release         <-chan struct{}
	cleanupRelease  <-chan struct{}
	cancelObserved  chan struct{}
}

func newCompactionConversation() *compactionConversation {
	return &compactionConversation{fakeConversation: &fakeConversation{stream: fixedStream()}, candidateTokens: 10}
}
func (conversation *compactionConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	revision, tokens := uint64(2), uint64(50000)
	if conversation.installed.Load() {
		revision, tokens = 3, conversation.candidateTokens
	}
	value, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, tokens)
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, revision, value)
}
func (conversation *compactionConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	revision := uint64(2)
	var summary domain.ContextSummary
	if conversation.installed.Load() {
		revision = 3
		summary, _ = domain.NewContextSummary("summary")
	}
	return domain.NewActiveContextView(conversation.ProjectHistory(), summary, revision)
}
func (conversation *compactionConversation) PreflightCompaction(input provider.CompactionInput) (provider.CompactionPreview, error) {
	if conversation.installed.Load() {
		return provider.CompactionPreview{}, provider.NewCompactionError(provider.CompactionNothing)
	}
	source, err := provider.NewCompactionSource(domain.ProviderOpenAI, "responses", "model", 2, 2, provider.RootCompactionPredecessor(), strings.Repeat("a", 64), provider.CompactionPromptRevision)
	if err != nil {
		return provider.CompactionPreview{}, err
	}
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 50000)
	return provider.NewCompactionPreview(source, input, tokens)
}
func (conversation *compactionConversation) RunCompaction(ctx context.Context, id domain.CompactionID, preview provider.CompactionPreview) (*provider.PreparedCompaction, error) {
	conversation.generated.Add(1)
	if conversation.entered != nil {
		close(conversation.entered)
	}
	if conversation.release != nil {
		select {
		case <-conversation.release:
		case <-ctx.Done():
			if conversation.cancelObserved != nil {
				close(conversation.cancelObserved)
			}
			if conversation.cleanupRelease != nil {
				<-conversation.cleanupRelease
			}
			return nil, ctx.Err()
		}
	}
	if conversation.generationErr != nil {
		return nil, conversation.generationErr
	}
	summary, _ := domain.NewContextSummary("summary")
	checkpoint, err := provider.NewCheckpointEnvelope(id, preview.Source(), 3, []byte(`{"native":"summary"}`))
	if err != nil {
		return nil, err
	}
	view, _ := domain.NewActiveContextView(conversation.ProjectHistory(), summary, 3)
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, conversation.candidateTokens)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 3, tokens)
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(50000), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(10), domain.UnknownUsageMetric())
	provenance, _ := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 3, []byte(`{"usage":10}`))
	return provider.NewPreparedCompaction(checkpoint, view, footprint, usage, provenance, func() { conversation.installed.Store(true) }, func() { conversation.discarded.Add(1) })
}

func TestCompactionDurableOrderAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name                  string
		failAt                int
		cancelAfterCompletion bool
		generationFailure     bool
		noReduction           bool
	}{
		{name: "success"}, {name: "cancel after admitted completion", cancelAfterCompletion: true}, {name: "started sync failure", failAt: 1}, {name: "completion sync failure", failAt: 2}, {name: "generation failure", generationFailure: true}, {name: "no reduction", noReduction: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conversation := newCompactionConversation()
			if test.generationFailure {
				conversation.generationErr = errors.New("private response and credential")
			}
			if test.noReduction {
				conversation.candidateTokens = 50000
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal := &fakeJournal{failAt: test.failAt}
			journal.hook = func(drafts []session.RecordDraft) {
				if drafts[0].EventKind() == session.EventCompactionStarted && conversation.generated.Load() != 0 {
					t.Error("network before started Sync")
				}
				if drafts[0].EventKind() == session.EventCompactionCompleted {
					if conversation.installed.Load() {
						t.Error("installed before completion Sync")
					}
					if test.cancelAfterCompletion {
						cancel()
					}
				}
			}
			runtime := newTestRuntime(t, conversation, journal)
			admission, code := runtime.preflightCompaction("")
			if code != "" {
				t.Fatal(code)
			}
			var events []protocol.Event
			err := runtime.runCompaction(ctx, "01900000-0000-7000-8000-000000000009", admission, func(event protocol.Event) { events = append(events, event) })
			success := test.failAt == 0 && !test.generationFailure && !test.noReduction
			if (err == nil) != success || conversation.installed.Load() != success {
				t.Fatalf("success=%t installed=%t err=%v", success, conversation.installed.Load(), err)
			}
			if len(events) == 0 {
				t.Fatal("missing unique terminal")
			}
			terminal := events[len(events)-1]
			if success {
				if terminal.Kind != protocol.EventCompactionCompleted || len(journal.snapshot()) != 2 {
					t.Fatal("completion ordering failed")
				}
			} else {
				failure, err := protocol.DecodeCompactionFailed(terminal)
				if err != nil {
					t.Fatal(err)
				}
				if failure.Durable != (test.failAt == 0) {
					t.Fatal("incorrect durability claim")
				}
				if (failure.UsageState == "observed") != (test.noReduction || test.failAt == 2) {
					t.Fatal("incorrect failure usage availability")
				}
				if strings.Contains(string(terminal.Payload), "private") {
					t.Fatal("response leaked")
				}
			}
			if test.failAt != 0 && !runtime.poisoned.Load() {
				t.Fatal("uncertain durability did not poison")
			}
			if test.failAt == 1 && conversation.generated.Load() != 0 {
				t.Fatal("network after start failure")
			}
			if conversation.calls.Load() != 0 {
				t.Fatal("compaction triggered an ordinary turn")
			}
		})
	}
}

func TestCompactionOwnerQueryAndTargetedCancel(t *testing.T) {
	conversation := newCompactionConversation()
	conversation.entered = make(chan struct{})
	conversation.release = make(chan struct{})
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, conversation, journal)
	loop, err := NewAgentLoop(runtime, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	state := newAgentLoopRunState(loop, context.Background())
	state.refreshContextSnapshot()
	request := func(command protocol.Command) protocol.CommandResult {
		t.Helper()
		reply := make(chan loopCommandResponse, 1)
		state.handleCommand(loopCommandRequest{command: command, result: reply})
		result := <-reply
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result.result
	}
	compact, _ := protocol.NewCompactContextCommand("compact", "")
	result := request(compact)
	if result.Status() != protocol.CommandStatusAccepted || !result.CompactionID().Valid() {
		t.Fatal("compact was not admitted")
	}
	event := <-state.compaction.events
	state.handleCompactionEvent(event, true)
	<-conversation.entered
	query, _ := protocol.NewQueryContextCommand("query")
	if request(query).Disposition() != protocol.CommandDispositionQueried {
		t.Fatal("query rejected while compacting")
	}
	if len(journal.snapshot()) != 1 || conversation.installed.Load() {
		t.Fatal("query changed window or journal")
	}
	stale, _ := protocol.NewCancelCompactionCommand("stale", "01900000-0000-7000-8000-000000000099")
	rejected := request(stale)
	failure, _ := rejected.Error()
	if failure.Code() != protocol.ControlErrorCompactionMismatch {
		t.Fatal("stale cancel was accepted")
	}
	cancel, _ := protocol.NewCancelCompactionCommand("cancel", result.CompactionID())
	if request(cancel).Disposition() != protocol.CommandDispositionCancelling {
		t.Fatal("cancel not admitted")
	}
	event = <-state.compaction.events
	if event.event.Kind != protocol.EventCompactionFailed {
		t.Fatal("cancel has no failure terminal")
	}
	state.handleCompactionEvent(event, true)
	state.handleCompactionFinished(<-state.compaction.finished, true)
	if state.compaction != nil || conversation.installed.Load() || state.runErr != nil {
		t.Fatal("cancel did not converge to healthy idle")
	}
	if len(journal.snapshot()) != 2 {
		t.Fatal("cancel not durably closed")
	}
}

func TestCompactionPreflightBudgetHasNoSideEffects(t *testing.T) {
	conversation := newCompactionConversation()
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, conversation, journal)
	runtime.budget, _ = contextplan.NewBudget(50000, 1, 0)
	_, code := runtime.preflightCompaction("")
	if code != protocol.ControlErrorCompactionBudgetExceeded || len(journal.snapshot()) != 0 || conversation.generated.Load() != 0 {
		t.Fatal("preflight wrote or sampled")
	}
}

func TestCompactionDeadlineAndFailedTerminalSync(t *testing.T) {
	for _, failSync := range []bool{false, true} {
		conversation := newCompactionConversation()
		conversation.release = make(chan struct{})
		journal := &fakeJournal{}
		if failSync {
			journal.failAt = 2
		}
		runtime := newTestRuntime(t, conversation, journal)
		runtime.compactionDeadline = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			if duration != 120*time.Second {
				t.Fatal("incorrect compaction deadline")
			}
			// 过去的 deadline 同步触发，测试无需等待真实计时器。
			return context.WithDeadline(ctx, time.Unix(0, 0))
		}
		admission, code := runtime.preflightCompaction("")
		if code != "" {
			t.Fatal(code)
		}
		var events []protocol.Event
		err := runtime.runCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", admission, func(e protocol.Event) { events = append(events, e) })
		if err == nil || len(events) != 2 || events[1].Kind != protocol.EventCompactionFailed || conversation.installed.Load() {
			t.Fatal("timeout lifecycle invalid")
		}
		failure, err := protocol.DecodeCompactionFailed(events[1])
		if err != nil || failure.Durable == failSync || failure.UsageState != "unavailable" {
			t.Fatalf("failure=%#v err=%v", failure, err)
		}
		if !failSync && failure.Code != protocol.CompactionFailureTimeout {
			t.Fatal("deadline lost")
		}
		if runtime.poisoned.Load() != failSync {
			t.Fatal("failed terminal Sync did not poison")
		}
	}
}

func TestCompactionShutdownRetainsCleanupOwner(t *testing.T) {
	conversation := newCompactionConversation()
	conversation.entered = make(chan struct{})
	conversation.release = make(chan struct{})
	cleanup := make(chan struct{})
	conversation.cleanupRelease = cleanup
	conversation.cancelObserved = make(chan struct{})
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	done := startAgentLoop(t, loop)
	command, _ := protocol.NewCompactContextCommand("compact", "")
	result, err := loop.Submit(context.Background(), command)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("not admitted")
	}
	<-conversation.entered
	shutdown, _ := protocol.NewShutdownCommand("shutdown")
	if _, err := loop.Submit(context.Background(), shutdown); err != nil {
		t.Fatal(err)
	}
	<-conversation.cancelObserved
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := loop.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	select {
	case <-loop.Done():
		t.Fatal("owner abandoned cleanup")
	default:
	}
	close(cleanup)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	terminal := 0
	for item := range loop.Outputs() {
		if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventCompactionFailed {
			terminal++
		}
	}
	if terminal != 1 || conversation.installed.Load() {
		t.Fatal("shutdown terminal invalid")
	}
}

func TestCompactionQueueFailureAndSuccessFIFO(t *testing.T) {
	for _, success := range []bool{false, true} {
		conversation := newCompactionConversation()
		conversation.entered = make(chan struct{})
		release := make(chan struct{})
		conversation.release = release
		if !success {
			conversation.generationErr = errors.New("generation failed")
		}
		loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
		state := newAgentLoopRunState(loop, context.Background())
		state.refreshContextSnapshot()
		request := func(command protocol.Command) protocol.CommandResult {
			reply := make(chan loopCommandResponse, 1)
			state.handleCommand(loopCommandRequest{command: command, result: reply})
			response := <-reply
			if response.err != nil {
				t.Fatal(response.err)
			}
			return response.result
		}
		compact, _ := protocol.NewCompactContextCommand("compact", "")
		if request(compact).Status() != protocol.CommandStatusAccepted {
			t.Fatal("not admitted")
		}
		state.handleCompactionEvent(<-state.compaction.events, true)
		<-conversation.entered
		auto, _ := protocol.NewSubmitConversationalInputCommand("auto", "pending automatic")
		strict, _ := protocol.NewSubmitInputCommand("strict", "pending strict")
		request(auto)
		request(strict)
		second, _ := protocol.NewCompactContextCommand("second", "")
		rejected := request(second)
		failure, _ := rejected.Error()
		if failure.Code() != protocol.ControlErrorSessionBusy {
			t.Fatal("second operation inserted")
		}
		state.startNext()
		if state.active != nil || len(state.queue) != 2 || conversation.calls.Load() != 0 {
			t.Fatal("queued input executed during summary")
		}
		close(release)
		state.handleCompactionEvent(<-state.compaction.events, true)
		state.handleCompactionFinished(<-state.compaction.finished, true)
		if state.active != nil {
			t.Fatal("completion started an implicit turn")
		}
		if success {
			if len(state.queue) != 2 || state.queue[0].requestID != "auto" || state.queue[1].requestID != "strict" {
				t.Fatal("success changed FIFO")
			}
		} else if len(state.queue) != 1 || state.queue[0].requestID != "strict" {
			t.Fatal("failure did not retain strict-next")
		}
		returned := 0
		for len(loop.outputs) > 0 {
			item := <-loop.outputs
			if status, ok := item.InputStatus(); ok && status.State() == protocol.InputReturned {
				returned++
			}
		}
		if (returned == 1) == success {
			t.Fatal("automatic return count wrong")
		}
	}
}
