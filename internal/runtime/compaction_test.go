package runtime

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type compactionConversation struct {
	policies chan domain.InferenceSettings
	*fakeConversation
	generated       atomic.Int32
	installed       atomic.Bool
	discarded       atomic.Int32
	candidateTokens uint64
	revision        uint64
	generationErr   error
	entered         chan struct{}
	release         <-chan struct{}
	cleanupRelease  <-chan struct{}
	cancelObserved  chan struct{}
}

func newCompactionConversation() *compactionConversation {
	return &compactionConversation{fakeConversation: &fakeConversation{stream: fixedStream()}, candidateTokens: 10, revision: 2}
}
func (conversation *compactionConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	revision, tokens := conversation.revision, uint64(50000)
	if conversation.installed.Load() {
		revision, tokens = conversation.revision+1, conversation.candidateTokens
	}
	value, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, tokens)
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, revision, value)
}
func (conversation *compactionConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	revision := conversation.revision
	var summary domain.ContextSummary
	if conversation.installed.Load() {
		revision = conversation.revision + 1
		summary, _ = domain.NewContextSummary("summary")
	}
	history := conversation.ProjectHistory()
	if conversation.installed.Load() {
		history.Turns = []domain.SemanticTurn{}
	}
	return domain.NewActiveContextView(history, summary, revision)
}

func (conversation *compactionConversation) ProjectContextWindow() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, err
	}
	view, err := conversation.ProjectActiveContext()
	return view, footprint, err
}
func (conversation *compactionConversation) PreflightCompaction(input provider.CompactionInput) (provider.CompactionPreview, provider.ContextWindowProjection, error) {
	if conversation.policies != nil {
		conversation.policies <- input.Sampling().InferenceSettings()
	}
	if conversation.installed.Load() {
		return provider.CompactionPreview{}, nil, provider.NewCompactionError(provider.CompactionNothing)
	}
	source, err := provider.NewCompactionSource(domain.ProviderOpenAI, "responses", "model", conversation.revision, conversation.revision, provider.RootCompactionPredecessor(), strings.Repeat("a", 64))
	if err != nil {
		return provider.CompactionPreview{}, nil, err
	}
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 50000)
	preview, err := provider.NewCompactionPreview(source, input, tokens)
	return preview, conversation.ProjectContextWindow, err
}
func (conversation *compactionConversation) RunCompaction(ctx context.Context, id domain.CompactionID, preview provider.CompactionPreview) (*provider.PreparedCompaction, error) {
	conversation.generated.Add(1)
	if conversation.policies != nil {
		conversation.policies <- preview.Input().Sampling().InferenceSettings()
	}
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
	checkpoint, err := provider.NewCheckpointEnvelope(preview.Input().Scope(), id, preview.Source(), []byte(`{"native":"summary"}`))
	if err != nil {
		return nil, err
	}
	view, _ := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{}}, summary, conversation.revision+1)
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, conversation.candidateTokens)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, conversation.revision+1, tokens)
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(50000), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(10), domain.UnknownUsageMetric())
	provenance, _ := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{"usage":10}`))
	return provider.NewPreparedCompaction(checkpoint, view, footprint, usage, provenance, syntheticRequestReceipt(domain.RequestSummary), func() { conversation.installed.Store(true) }, func() { conversation.discarded.Add(1) })
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
				requireNoError(t, err)
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

func TestCompactionShutdownRetainsCleanupOwner(t *testing.T) {
	t.Run("provider stream", func(t *testing.T) { testCompactionShutdownRetainsOwner(t, "") })
	t.Run("pre-compact command", func(t *testing.T) { testCompactionShutdownRetainsOwner(t, domain.HookPreCompact) })
	t.Run("post-compact command", func(t *testing.T) { testCompactionShutdownRetainsOwner(t, domain.HookPostCompact) })
}

type compactionCleanupRunner struct {
	entered, cancelled, release chan struct{}
}

func (*compactionCleanupRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }

func (runner *compactionCleanupRunner) Run(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	close(runner.entered)
	<-ctx.Done()
	close(runner.cancelled)
	<-runner.release
	return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true, Origin: "execution"}}
}

func testCompactionShutdownRetainsOwner(t *testing.T, hookEvent domain.HookEvent) {
	t.Helper()
	conversation := newCompactionConversation()
	entered, cancelled, cleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	switch hookEvent {
	case domain.HookPreCompact:
		runner := &compactionCleanupRunner{entered, cancelled, cleanup}
		declaration := installPreCompactFixture(t, loop.runtime, runner, true)
		loop.runtime.hooks.trust[declaration.Reference.Key()] = protocol.HookTrustSession
	case domain.HookPostCompact:
		scope, _ := domain.ParseCompactionHookScope("manual")
		declaration, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "observe", scope, "true", 5)
		requireNoError(t, err)
		installRuntimeTestHooks(t, loop.runtime, &compactionCleanupRunner{entered, cancelled, cleanup}, t.TempDir(), []domain.HookDeclaration{declaration})
		loop.runtime.interactiveApprovals = true
		loop.runtime.hooks.trust[declaration.Reference.Key()] = protocol.HookTrustSession
	default:
		conversation.entered, conversation.cancelObserved = entered, cancelled
		conversation.release, conversation.cleanupRelease = make(chan struct{}), cleanup
	}
	done := startAgentLoop(t, loop)
	command, _ := protocol.NewCompactContextCommand("compact", "")
	result, err := loop.Submit(context.Background(), command)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("not admitted")
	}
	<-entered
	shutdown, _ := protocol.NewShutdownCommand("shutdown")
	if _, err := loop.Submit(context.Background(), shutdown); err != nil {
		t.Fatal(err)
	}
	<-cancelled
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
	wantTerminal := protocol.EventCompactionFailed
	if hookEvent == domain.HookPostCompact {
		wantTerminal = protocol.EventCompactionCompleted
	}
	for item := range loop.Outputs() {
		if event, ok := item.ContextEvent(); ok {
			switch event.Kind {
			case protocol.EventCompactionFailed, protocol.EventCompactionCompleted:
				if event.Kind != wantTerminal {
					t.Fatalf("unexpected terminal: %s", event.Kind)
				}
				terminal++
			}
		}
	}
	if terminal != 1 || conversation.installed.Load() != (hookEvent == domain.HookPostCompact) {
		t.Fatal("shutdown terminal invalid")
	}
	if hookEvent == domain.HookPreCompact && conversation.generated.Load() != 0 {
		t.Fatal("cancelled gate reached the provider")
	}
}

func (conversation *compactionConversation) PrepareUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedUserInputs, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return nil, err
	}
	return prepareFixtureInputs(a, inputs, conversation.ProjectHistory(), footprint, func(view domain.SemanticHistoryView) { conversation.history = view })
}
