package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func TestAutomaticCompactionKeepsTurnAndDurableBoundary(t *testing.T) {
	for _, mode := range []string{"success", "failure", "no_reduction", "cancel_after_sync", "sync_failure"} {
		t.Run(mode, func(t *testing.T) {
			conversation := newCompactionConversation()
			if mode == "failure" {
				conversation.generationErr = errors.New("private network body")
			}
			if mode == "no_reduction" {
				conversation.candidateTokens = 50000
			}
			journal := &fakeJournal{}
			if mode == "sync_failure" {
				journal.failAt = 2
			}
			runtime := newTestRuntime(t, conversation, journal)
			budget, _ := contextplan.NewBudget(60000, 0, 0)
			runtime.budget = budget
			runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, budget, 4096)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal.hook = func(drafts []session.RecordDraft) {
				if mode == "cancel_after_sync" && drafts[0].EventKind() == session.EventCompactionCompleted {
					cancel()
				}
			}
			var events []protocol.Event
			err := runtime.prepareAutomaticSample(ctx, runtimeTurnID, 1, nil, func(e protocol.Event) { events = append(events, e) })
			if (err != nil) != (mode == "cancel_after_sync" || mode == "sync_failure") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if conversation.generated.Load() != 1 {
				t.Fatal("wrong request count")
			}
			installed := mode == "success" || mode == "cancel_after_sync"
			if conversation.installed.Load() != installed {
				t.Fatal("wrong durable window")
			}
			for _, event := range events {
				if event.TurnID != runtimeTurnID || !event.CompactionID.Valid() || event.Validate() != nil {
					t.Fatalf("scope lost: %#v", event)
				}
				if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnStarted {
					t.Fatal("summary invented turn lifecycle")
				}
			}
			if mode == "failure" || mode == "no_reduction" {
				if runtime.autoCompactionFailures != 1 {
					t.Fatal("failure uncounted")
				}
				if err := runtime.prepareAutomaticSample(ctx, runtimeTurnID, 2, nil, func(protocol.Event) {}); err != nil {
					t.Fatal(err)
				}
				if conversation.generated.Load() != 1 {
					t.Fatal("same source retried")
				}
			}
		})
	}
}

func TestAutomaticFailureCircuitPersistsAcrossRevisions(t *testing.T) {
	conversation := newCompactionConversation()
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, conversation, journal)
	budget, _ := contextplan.NewBudget(60000, 0, 0)
	runtime.budget = budget
	runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, budget, 4096)
	conversation.generationErr = errors.New("network failure")
	for index := range 3 {
		conversation.revision = uint64(index + 2)
		if err := runtime.prepareAutomaticSample(context.Background(), runtimeTurnID, uint32(index), nil, func(protocol.Event) {}); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.prepareAutomaticSample(context.Background(), runtimeTurnID, 3, nil, func(protocol.Event) {}); err != nil {
		t.Fatal(err)
	}
	if conversation.generated.Load() != 3 || len(journal.snapshot()) != 6 {
		t.Fatal("failure counter reset across revisions or circuit did not block work")
	}
	conversation.generationErr = nil

	admission, code := runtime.preflightCompaction("")
	if code != "" {
		t.Fatal(code)
	}
	if err := runtime.runCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", admission, func(protocol.Event) {}); err != nil {
		t.Fatal(err)
	}
	if runtime.autoCompactionFailures != 0 || runtime.autoCompactionFailedRevision != 0 {
		t.Fatal("manual success did not reset circuit")
	}
}

// automaticToolConversation 组合既有工具与摘要测试边界，共享同一原生会话。
type automaticToolConversation struct {
	*fakeToolConversation
	compact *compactionConversation
}

func (c *automaticToolConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	return c.compact.ProjectActiveContext()
}
func (c *automaticToolConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	return c.compact.HistoryFootprint()
}
func (c *automaticToolConversation) PreflightCompaction(input provider.CompactionInput) (provider.CompactionPreview, error) {
	return c.compact.PreflightCompaction(input)
}
func (c *automaticToolConversation) RunCompaction(ctx context.Context, id domain.CompactionID, preview provider.CompactionPreview) (*provider.PreparedCompaction, error) {
	return c.compact.RunCompaction(ctx, id, preview)
}

func TestAutomaticCompactionAfterDurableToolOutputs(t *testing.T) {
	base := &fakeConversation{}
	compact := newCompactionConversation()
	compact.fakeConversation = base
	conversation := &automaticToolConversation{fakeToolConversation: &fakeToolConversation{fakeConversation: base}, compact: compact}
	first := newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-1"))
	second := newPreparedSample(t, func() {})
	base.stream = func(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
		if base.calls.Load() == 1 {
			return fixedStream(completedProviderEventWithSample(t, first))(ctx, input)
		}
		if !compact.installed.Load() {
			t.Error("continuation before checkpoint installation")
		}
		return fixedStream(completedProviderEventWithSample(t, second))(ctx, input)
	}
	outputsDurable := false
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		if len(drafts) == 1 && drafts[0].EventKind() == session.EventProviderNativeCommit {
			outputsDurable = true
		}
		if drafts[0].EventKind() == session.EventCompactionStarted && !outputsDurable {
			t.Error("summary before durable tool pairing")
		}
	}}
	runtime := newTestRuntime(t, conversation, journal)
	budget, _ := contextplan.NewBudget(60000, 0, 0)
	runtime.budget = budget
	runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, budget, 4096)
	events, err := collectTurn(runtime, context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if compact.generated.Load() != 1 || base.calls.Load() != 2 || len(conversation.results) != 1 {
		t.Fatal("duplicate work or missing continuation")
	}
	count := 0
	for _, event := range events {
		if event.Kind == protocol.EventCompactionStarted {
			payload, err := protocol.DecodeCompactionStarted(event)
			if err != nil || payload.Scope.SampleIndex != 1 {
				t.Fatal("wrong sample association")
			}
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate compaction")
	}
}

func TestAutomaticCompactionControlOwnerAndCleanup(t *testing.T) {
	conversation := newCompactionConversation()
	conversation.entered = make(chan struct{})
	conversation.release = make(chan struct{})
	cleanup := make(chan struct{})
	conversation.cleanupRelease = cleanup
	conversation.cancelObserved = make(chan struct{})
	runtime := newTestRuntime(t, conversation, &fakeJournal{})
	budget, _ := contextplan.NewBudget(60000, 0, 0)
	runtime.budget = budget
	runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, budget, 4096)
	loop, err := NewAgentLoop(runtime, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := startAgentLoop(t, loop)
	var cleanupOnce sync.Once
	t.Cleanup(func() { cleanupOnce.Do(func() { close(cleanup) }); _ = loop.Stop(context.Background()) })
	submit, _ := protocol.NewSubmitInputCommand("input", "continue work")
	if _, err := loop.Submit(context.Background(), submit); err != nil {
		t.Fatal(err)
	}
	<-conversation.entered
	query, _ := protocol.NewQueryContextCommand("query")
	if result, err := loop.Submit(context.Background(), query); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("query blocked", err)
	}
	compact, _ := protocol.NewCompactContextCommand("manual", "")
	if result, err := loop.Submit(context.Background(), compact); err != nil || result.Status() != protocol.CommandStatusRejected {
		t.Fatal("manual overlaps turn", err)
	}
	shutdown, _ := protocol.NewShutdownCommand("shutdown")
	if _, err := loop.Submit(context.Background(), shutdown); err != nil {
		t.Fatal(err)
	}
	<-conversation.cancelObserved
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := loop.Stop(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("owner abandoned cleanup", err)
	}
	select {
	case <-loop.Done():
		t.Fatal("cleanup ownership lost")
	default:
	}
	cleanupOnce.Do(func() { close(cleanup) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	started, failed, turnFailed := 0, 0, 0
	snapshotSeen := false
	for item := range loop.Outputs() {
		if e, ok := item.ContextEvent(); ok {
			switch e.Kind {
			case protocol.EventContextSnapshot:
				snapshot, err := protocol.DecodeContextSnapshot(e)
				if err != nil || snapshot.Activity != "compacting" {
					t.Fatal("incoherent snapshot", err)
				}
				snapshotSeen = true
			case protocol.EventCompactionStarted:
				started++
			case protocol.EventCompactionFailed:
				failed++
			}
		}
		if e, ok := item.TurnEvent(); ok && e.Kind == protocol.EventTurnFailed {
			turnFailed++
		}
	}
	if !snapshotSeen || started != 1 || failed != 1 || turnFailed != 1 || conversation.calls.Load() != 0 {
		t.Fatal("invalid terminal or continuation after cancellation")
	}
}

func TestAutomaticFailureReturnsQueuedAutoInputs(t *testing.T) {
	conversation := newCompactionConversation()
	conversation.generationErr = errors.New("network failure")
	conversation.entered = make(chan struct{})
	release := make(chan struct{})
	conversation.release = release
	runtime := newTestRuntime(t, conversation, &fakeJournal{})
	runtime.budget, _ = contextplan.NewBudget(60000, 20000, 0)
	runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, runtime.budget, 4096)
	loop, err := NewAgentLoop(runtime, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := startAgentLoop(t, loop)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = loop.Stop(context.Background()) })
	submit, _ := protocol.NewSubmitInputCommand("initial", "work")
	if _, err := loop.Submit(context.Background(), submit); err != nil {
		t.Fatal(err)
	}
	<-conversation.entered
	queued, _ := protocol.NewSubmitConversationalInputCommand("queued", "preserve queued correction")
	if _, err := loop.Submit(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	returned := false
	for !returned {
		item, ok := <-loop.Outputs()
		if !ok {
			t.Fatal("owner exited before returning queue")
		}
		if status, ok := item.InputStatus(); ok && status.RequestID() == "queued" && status.State() == protocol.InputReturned {
			returned = true
		}
		if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventTurnStarted && event.TurnID != runtimeTurnID {
			t.Fatal("queued correction executed after failure")
		}
	}
	if conversation.calls.Load() != 0 {
		t.Fatal("ordinary sample despite hard limit")
	}
	if err := loop.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
