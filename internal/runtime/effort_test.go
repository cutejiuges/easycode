package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func effortChoice(t *testing.T, level string) domain.EffortChoice {
	t.Helper()
	c, err := domain.NewExplicitEffortChoice(domain.EffortLevel(level))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func effortLoop(t *testing.T, c provider.Conversation, j Journal) *AgentLoop {
	t.Helper()
	cfg := testRuntimeConfig(t, j)
	cfg.GenerateTurnID = sequentialTurnIDs()
	settings, err := domain.NewOpenAIInference("high", "auto", 16384)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ContextProfile, err = cfg.ContextProfile.WithInferenceSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	return loop
}
func effortRequest(t *testing.T, s *agentLoopRunState, command protocol.Command) (protocol.CommandResult, *protocol.EffortSnapshotPayload) {
	t.Helper()
	request := loopCommandRequest{command: command, result: make(chan loopCommandResponse, 1)}
	s.handleCommand(request)
	response := <-request.result
	if response.err != nil {
		t.Fatal(response.err)
	}
	var snapshot *protocol.EffortSnapshotPayload
	for len(s.loop.outputs) > 0 {
		item := <-s.loop.outputs
		if e, ok := item.ContextEvent(); ok && e.Kind == protocol.EventEffortSnapshot {
			p, err := protocol.DecodeEffortSnapshot(e)
			if err != nil {
				t.Fatal(err)
			}
			snapshot = &p
		}
	}
	return response.result, snapshot
}
func TestEffortOwnerAtomicChoicesOverflowObservationAndNoFacts(t *testing.T) {
	j := &fakeJournal{}
	c := newControlledConversation()
	loop := effortLoop(t, c, j)
	state := newAgentLoopRunState(loop, context.Background())
	state.refreshContextSnapshot()
	explicit, _ := protocol.NewSetEffortCommand("explicit", effortChoice(t, "high"))
	result, snapshot := effortRequest(t, state, explicit)
	if result.Disposition() != protocol.CommandDispositionUpdated || snapshot.Source != "runtime" || snapshot.SelectionRevision != 1 || snapshot.Pending {
		t.Fatal("source change lost", snapshot)
	}
	same, _ := protocol.NewSetEffortCommand("same", effortChoice(t, "high"))
	_, snapshot = effortRequest(t, state, same)
	if snapshot.SelectionRevision != 1 {
		t.Fatal("same choice advanced sequence")
	}
	result, _ = effortRequest(t, state, same)
	assertRejectedCode(t, result, protocol.ControlErrorDuplicateRequest)
	loop.effortSnapshot = func(domain.SessionID, domain.ThreadID, protocol.EffortSnapshotPayload) (protocol.Event, error) {
		return protocol.Event{}, errors.New("encoding fixture")
	}
	low, _ := protocol.NewSetEffortCommand("failed-constructor", effortChoice(t, "low"))
	result, _ = effortRequest(t, state, low)
	assertRejectedCode(t, result, protocol.ControlErrorInvalidCommand)
	if state.selectionRevision != 1 || state.selectedProfile.InferenceSettings().Effort() != "high" {
		t.Fatal("failed preflight mutated selection")
	}
	loop.effortSnapshot = protocol.NewEffortSnapshot
	originalPlanner := loop.runtime.planner
	loop.runtime.planner = contextPlannerFunc(func(contextplan.PlanningInput) (contextplan.ContextPlan, error) {
		return contextplan.ContextPlan{}, errors.New("observation fixture")
	})
	low, _ = protocol.NewSetEffortCommand("low", effortChoice(t, "low"))
	result, snapshot = effortRequest(t, state, low)
	if result.Status() != protocol.CommandStatusAccepted || snapshot.Selected.Value != "low" || state.contextSnapshotErr == nil {
		t.Fatal("observation failure changed admission")
	}
	query, _ := protocol.NewQueryEffortCommand("query")
	_, snapshot = effortRequest(t, state, query)
	if snapshot.Selected.Value != "low" {
		t.Fatal("effort query depended on context")
	}
	ctxCommand, _ := protocol.NewQueryContextCommand("bad-context")
	result, _ = effortRequest(t, state, ctxCommand)
	assertRejectedCode(t, result, protocol.ControlErrorInvalidCommand)
	loop.runtime.planner = originalPlanner
	state.selectionRevision = math.MaxUint64
	reset, _ := protocol.NewSetEffortCommand("overflow", domain.NewResetEffortChoice())
	result, _ = effortRequest(t, state, reset)
	assertRejectedCode(t, result, protocol.ControlErrorInvalidCommand)
	same, _ = protocol.NewSetEffortCommand("same-at-limit", effortChoice(t, "low"))
	result, snapshot = effortRequest(t, state, same)
	if result.Status() != protocol.CommandStatusAccepted || snapshot.SelectionRevision != math.MaxUint64 {
		t.Fatal("no-op at limit failed")
	}
	if len(j.snapshot()) != 0 || len(c.calls) != 0 {
		t.Fatal("settings caused facts or requests")
	}
	for _, mode := range []loopMode{loopModeDraining, loopModeClosing} {
		state.mode = mode
		cmd, _ := protocol.NewSetEffortCommand(protocol.RequestID("closed-"+string(rune('0'+mode))), domain.NewAutomaticEffortChoice())
		result, _ = effortRequest(t, state, cmd)
		assertRejectedCode(t, result, protocol.ControlErrorSessionClosing)
	}
}

func queryEffortLoop(t *testing.T, loop *AgentLoop, id protocol.RequestID) protocol.EffortSnapshotPayload {
	t.Helper()
	cmd, _ := protocol.NewQueryEffortCommand(id)
	if result, err := loop.Submit(context.Background(), cmd); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("query failed", result, err)
	}
	for {
		select {
		case item := <-loop.Outputs():
			if e, ok := item.ContextEvent(); ok && item.RequestID() == id && e.Kind == protocol.EventEffortSnapshot {
				p, err := protocol.DecodeEffortSnapshot(e)
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
		case <-time.After(5 * time.Second):
			t.Fatal("effort query timed out")
		}
	}
}
func setEffortLoop(t *testing.T, loop *AgentLoop, id protocol.RequestID, choice domain.EffortChoice) {
	t.Helper()
	command, _ := protocol.NewSetEffortCommand(id, choice)
	if result, err := loop.Submit(context.Background(), command); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("setting rejected", result, err)
	}
}

func TestEffortTurnCaptureQueueAndCancelledHandoff(t *testing.T) {
	j := &fakeJournal{}
	c := newControlledConversation()
	loop := effortLoop(t, c, j)
	done := startAgentLoop(t, loop)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	command, _ := protocol.NewSetEffortCommand("cancelled", effortChoice(t, "low"))
	if _, err := loop.Submit(cancelled, command); err == nil {
		t.Fatal("cancelled handoff accepted")
	}
	submitLoopCommand(t, loop, "first", "first")
	call := receiveInvocation(t, c.calls)
	if call.settings.Effort() != "high" {
		t.Fatal("startup not captured")
	}
	setEffortLoop(t, loop, "low", effortChoice(t, "low"))
	snapshot := queryEffortLoop(t, loop, "active")
	if snapshot.Active == nil || snapshot.Active.Effort().Value != "high" || snapshot.Selected.Value != "low" || !snapshot.Pending || snapshot.Active.SelectionRevision() != 0 {
		t.Fatal("active policy changed", snapshot)
	}
	submitLoopCommand(t, loop, "second", "second")
	call.release <- completedProviderEvent(t)
	next := receiveInvocation(t, c.calls)
	if next.settings.Effort() != "low" || next.settings.Summary() != "auto" || next.settings.MaxOutputTokens() != 16384 {
		t.Fatal("queued capture lost frozen fields")
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	next.release <- completedProviderEvent(t)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c.maxActive.Load() != 1 {
		t.Fatal("settings created concurrency")
	}
}

type effortStartedJournal struct {
	*fakeJournal
	entered chan struct{}
	release <-chan struct{}
}

func (j *effortStartedJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if len(drafts) == 1 && drafts[0].EventKind() == session.EventTurnStarted {
		close(j.entered)
		<-j.release
	}
	return j.fakeJournal.AppendBatch(ctx, drafts)
}
func TestEffortStartingCapturePrecedesDurableIdentity(t *testing.T) {
	release := make(chan struct{})
	j := &effortStartedJournal{fakeJournal: &fakeJournal{}, entered: make(chan struct{}), release: release}
	c := newControlledConversation()
	loop := effortLoop(t, c, j)
	done := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "original-request", "work")
	<-j.entered
	setEffortLoop(t, loop, "later", effortChoice(t, "low"))
	p := queryEffortLoop(t, loop, "starting")
	if p.Active.Kind() != "turn_starting" || p.Active.RequestID() != "original-request" || p.Active.TurnID() != "" || p.Active.Phase() != "starting" || p.Active.Effort().Value != "high" || !p.Pending || len(c.calls) != 0 {
		t.Fatal("premature identity or request", p)
	}
	close(release)
	call := receiveInvocation(t, c.calls)
	p = queryEffortLoop(t, loop, "running")
	if p.Active.Kind() != "turn" || p.Active.TurnID() == "" {
		t.Fatal("durable identity not installed")
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	call.release <- completedProviderEvent(t)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type effortCleanupConversation struct {
	*fakeConversation
	entered chan struct{}
	release <-chan struct{}
}

func (c *effortCleanupConversation) ResetContinuation() { close(c.entered); <-c.release }
func TestEffortFinishingRetainsCaptureUntilWorkerJoin(t *testing.T) {
	release := make(chan struct{})
	failed, _ := provider.NewFailedStreamEvent(errors.New("fixture failure"))
	c := &effortCleanupConversation{fakeConversation: &fakeConversation{stream: fixedStream(failed)}, entered: make(chan struct{}), release: release}
	loop := effortLoop(t, c, &fakeJournal{})
	done := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "work", "work")
	receiveTurnEvent(t, loop.Outputs(), "work", protocol.EventTurnFailed)
	<-c.entered
	setEffortLoop(t, loop, "next", effortChoice(t, "low"))
	p := queryEffortLoop(t, loop, "finishing")
	if p.Active == nil || p.Active.Phase() != "finishing" || p.Active.Effort().Value != "high" || !p.Pending {
		t.Fatal("terminal became idle", p)
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEffortManualAndAutomaticCompactionCapture(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("auto%t/fail%t", automatic, fail), func(t *testing.T) {
				release := make(chan struct{})
				c := newCompactionConversation()
				c.entered = make(chan struct{})
				c.release = release
				c.policies = make(chan domain.InferenceSettings, 8)
				if fail {
					c.generationErr = errors.New("summary fixture")
				}
				c.fakeConversation.stream = fixedStream(completedProviderEvent(t))
				loop := effortLoop(t, c, &fakeJournal{})
				if automatic {
					loop.runtime.budget, _ = contextplan.NewBudget(70000, 16384, 0)
					loop.runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, loop.runtime.budget, 4096)
				}
				done := startAgentLoop(t, loop)
				if automatic {
					submitLoopCommand(t, loop, "task", "new work")
				} else {
					command, _ := protocol.NewCompactContextCommand("summary", "")
					if result, err := loop.Submit(context.Background(), command); err != nil || result.Status() != protocol.CommandStatusAccepted {
						t.Fatal("summary admission failed", result, err)
					}
				}
				<-c.entered
				setEffortLoop(t, loop, "low", effortChoice(t, "low"))
				p := queryEffortLoop(t, loop, "compacting")
				kind := "manual_compaction"
				if automatic {
					kind = "turn"
				}
				if p.Activity != "compacting" || p.Active.Kind() != kind || p.Active.Effort().Value != "high" || !p.Pending {
					t.Fatal("compaction changed capture", p)
				}
				before, execution := <-c.policies, <-c.policies
				if before.Effort() != "high" || execution != before {
					t.Fatal("summary preflight and execution differed")
				}
				close(release)
				if err := loop.CloseInput(context.Background()); err != nil {
					t.Fatal(err)
				}
				go drainControlItems(loop.Outputs())
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if loop.runtime.autoCompactionFailures != 0 && !fail {
					t.Fatal("successful summary changed suppression")
				}
				if automatic && fail && loop.runtime.autoCompactionFailures != 1 {
					t.Fatal("effort reset failure count")
				}
				if loop.runtime.profile.InferenceSettings().Effort() != "high" || c.generated.Load() != 1 {
					t.Fatal("startup mutated or summary repeated")
				}
			})
		}
	}
}

func TestEffortPreservesAutoCompactionBreaker(t *testing.T) {
	loop := effortLoop(t, newControlledConversation(), &fakeJournal{})
	s := newAgentLoopRunState(loop, context.Background())
	loop.runtime.autoCompactionFailures = 3
	loop.runtime.autoCompactionFailedRevision = 9
	command, _ := protocol.NewSetEffortCommand("low", effortChoice(t, "low"))
	result, _ := effortRequest(t, s, command)
	if result.Status() != protocol.CommandStatusAccepted || loop.runtime.autoCompactionFailures != 3 || loop.runtime.autoCompactionFailedRevision != 9 {
		t.Fatal("setting reset circuit breaker")
	}
}

func TestEffortStopContinuationUsesCapturedPolicy(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic-summary-%t", automatic), func(t *testing.T) { runEffortStopCapture(t, automatic) })
	}
}

func runEffortStopCapture(t *testing.T, automatic bool) {
	bodies := make(chan []byte, 8)
	j := &fakeJournal{}
	r, runner, requests := newStopRuntimeRequestFixture(t, j, func(body []byte) { bodies <- body })
	settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
	r.profile, _ = r.profile.WithInferenceSettings(settings)
	r.operationProfile = r.profile
	r.newTurnID = sequentialTurnIDs()
	if automatic {
		j.hook = func(drafts []session.RecordDraft) {
			for _, draft := range drafts {
				if draft.EventKind() != session.EventTurnStopHookDecided {
					continue
				}
				decision, err := domain.DecodeStopDecision(draft.PayloadBytes())
				if err != nil || decision.Disposition != domain.StopContinued || len(runner.inputs) != 1 {
					continue
				}
				plan, err := r.contextPlan()
				if err != nil {
					t.Fatal(err)
				}
				base, known := plan.TotalEstimate().Tokens()
				if !known {
					t.Fatal("fixture estimate is unknown")
				}
				r.budget, err = contextplan.NewBudget(16384+base+500, 16384, 0)
				if err != nil {
					t.Fatal(err)
				}
				r.autoCompactionPolicy, err = contextplan.NewAutoCompactionPolicy(true, r.budget, 4096)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := startAgentLoop(t, loop)
	text := "complete the task"
	if automatic {
		text += strings.Repeat(" preserve original details ", 3000)
	}
	submitLoopCommand(t, loop, "task", text)
	changed, summaries := false, 0
	var capturedTurn domain.TurnID
	for item := range loop.Outputs() {
		event, ok := item.TurnEvent()
		if !ok {
			event, ok = item.ContextEvent()
		}
		if !ok {
			continue
		}
		if event.Kind == protocol.EventTurnStarted {
			capturedTurn = event.TurnID
		}
		if event.Kind == protocol.EventHookTrustRequested {
			setEffortLoop(t, loop, "low", effortChoice(t, "low"))
			changed = true
			hookTrustEmitter(t, r, protocol.HookTrustSession, nil)(event)
		}
		if event.Kind == protocol.EventTurnCompleted {
			break
		}
		if event.Kind == protocol.EventCompactionStarted {
			summaries++
			if !capturedTurn.Valid() || event.TurnID != capturedTurn {
				t.Fatal("Stop summary lost its captured root turn")
			}
		}
	}
	want := int32(3)
	if automatic {
		want++
		if summaries != 1 {
			t.Fatal("Stop did not generate exactly one automatic summary", summaries)
		}
	}
	if !changed || len(runner.inputs) != 3 || requests.Load() != want {
		t.Fatal("Stop continuation changed execution count")
	}
	for range want {
		body := <-bodies
		if !bytes.Contains(body, []byte(`"effort":"high"`)) {
			t.Fatal("Stop adopted next effort", string(body))
		}
		if bytes.Contains(body, []byte(`"tool_choice":"none"`)) && !bytes.Contains(body, []byte(`"max_output_tokens":4096`)) {
			t.Fatal("Stop summary lost its independent output cap")
		}
	}
	for i := 0; ; i++ {
		p := queryEffortLoop(t, loop, protocol.RequestID(fmt.Sprintf("idle-stop-%d", i)))
		if p.Activity == "idle" {
			break
		}
	}
	submitLoopCommand(t, loop, "next", "next task")
	receiveTurnEvent(t, loop.Outputs(), "next", protocol.EventTurnCompleted)
	if body := <-bodies; !bytes.Contains(body, []byte(`"effort":"low"`)) {
		t.Fatal("next turn did not use selected effort")
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEffortFailedStartedSyncFinishesWithoutDurableTurnIdentity(t *testing.T) {
	startedRelease := make(chan struct{})
	cleanupRelease := make(chan struct{})
	journal := &effortStartedJournal{fakeJournal: &fakeJournal{failAt: 1}, entered: make(chan struct{}), release: startedRelease}
	c := &effortCleanupConversation{fakeConversation: &fakeConversation{stream: fixedStream()}, entered: make(chan struct{}), release: cleanupRelease}
	loop := effortLoop(t, c, journal)
	done := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "original", "work")
	<-journal.entered
	close(startedRelease)
	receiveTurnEvent(t, loop.Outputs(), "original", protocol.EventTurnFailed)
	<-c.entered
	p := queryEffortLoop(t, loop, "failed-start")
	if p.Active == nil || p.Active.Kind() != "turn_starting" || p.Active.Phase() != "finishing" || p.Active.RequestID() != "original" || p.Active.TurnID() != "" {
		t.Fatal("failed Sync invented durable turn identity", p)
	}
	command, _ := protocol.NewSetEffortCommand("poisoned", effortChoice(t, "low"))
	result, err := loop.Submit(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorSessionFailed)
	go drainControlItems(loop.Outputs())
	close(cleanupRelease)
	if err := <-done; err == nil {
		t.Fatal("failed Sync did not poison session")
	}
	if len(journal.snapshot()) != 0 || c.calls.Load() != 0 {
		t.Fatal("failed start produced committed facts or network")
	}
}

func TestEffortCancelRequestedDoesNotClearCapturedOperation(t *testing.T) {
	release := make(chan struct{})
	c := newControlledConversation()
	c.cancelObserved = make(chan struct{}, 1)
	c.cancelRelease = release
	loop := effortLoop(t, c, &fakeJournal{})
	done := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "work", "work")
	receiveInvocation(t, c.calls)
	setEffortLoop(t, loop, "low", effortChoice(t, "low"))
	p := queryEffortLoop(t, loop, "before-cancel")
	command, _ := protocol.NewInterruptCommand("cancel", p.Active.TurnID())
	if result, err := loop.Submit(context.Background(), command); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("interrupt rejected", err)
	}
	<-c.cancelObserved
	p = queryEffortLoop(t, loop, "after-cancel")
	if p.Active == nil || !p.Active.CancelRequested() || p.Active.Phase() != "running" || p.Active.Effort().Value != "high" || p.Selected.Value != "low" || !p.Pending {
		t.Fatal("cancel acknowledgement became cleanup", p)
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type effortPreNetworkCompactionJournal struct {
	*fakeJournal
	entered chan struct{}
}

func (j *effortPreNetworkCompactionJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if len(drafts) == 1 && drafts[0].EventKind() == session.EventCompactionStarted {
		close(j.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return j.fakeJournal.AppendBatch(ctx, drafts)
}

type effortResetCompactionConversation struct {
	*compactionConversation
	resets atomic.Int32
}

func (c *effortResetCompactionConversation) ResetContinuation() { c.resets.Add(1) }

func TestEffortManualCancellationBeforeNetworkResetsContinuation(t *testing.T) {
	j := &effortPreNetworkCompactionJournal{fakeJournal: &fakeJournal{}, entered: make(chan struct{})}
	c := &effortResetCompactionConversation{compactionConversation: newCompactionConversation()}
	loop := effortLoop(t, c, j)
	done := startAgentLoop(t, loop)
	setEffortLoop(t, loop, "low", effortChoice(t, "low"))
	command, _ := protocol.NewCompactContextCommand("summary", "")
	result, err := loop.Submit(context.Background(), command)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("manual admission failed", result, err)
	}
	<-j.entered
	setEffortLoop(t, loop, "next-high", effortChoice(t, "high"))
	cancel, _ := protocol.NewCancelCompactionCommand("cancel-summary", result.CompactionID())
	if result, err := loop.Submit(context.Background(), cancel); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("manual cancellation failed", err)
	}
	for i := 0; ; i++ {
		p := queryEffortLoop(t, loop, protocol.RequestID(fmt.Sprintf("manual-idle-%d", i)))
		if p.Activity == "idle" {
			break
		}
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c.generated.Load() != 0 || len(j.snapshot()) != 0 {
		t.Fatal("cancelled initial Sync performed summary I/O")
	}
	if c.resets.Load() != 1 {
		t.Fatalf("failed admitted compaction retained continuation: resets=%d", c.resets.Load())
	}
}
