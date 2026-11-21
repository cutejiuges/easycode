package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type postCompactTestRunner struct {
	run func(context.Context, domain.HookRunIdentity) domain.HookRunResult
}

type postCompactToolFixture struct {
	*compactionConversation
	outputs fakeToolConversation
}

func (fixture *postCompactToolFixture) PrepareToolOutputs(results []tool.ModelOutput) (*provider.PreparedToolOutputs, error) {
	return fixture.outputs.PrepareToolOutputs(results)
}

func (fixture *postCompactToolFixture) PrepareToolOutputPreflight(invocation tool.Invocation) (provider.ToolOutputPreflight, error) {
	return fixture.outputs.PrepareToolOutputPreflight(invocation)
}

func TestPostCompactPoisonStopsToolContinuation(t *testing.T) {
	conversation := &postCompactToolFixture{compactionConversation: newCompactionConversation()}
	conversation.stream = fixedStream(completedProviderEventWithSample(t, newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-observed"))))
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, conversation, journal)
	runtime.budget, _ = contextplan.NewBudget(60000, 0, 0)
	policy, _ := contextplan.NewAutoCompactionPolicy(true, runtime.budget, 4096)
	// 首次采样完成工具调用后，再允许在已提交 outputs 边界尝试压缩。
	journal.hook = func(drafts []session.RecordDraft) {
		if drafts[0].EventKind() == session.EventToolCallResult {
			runtime.autoCompactionPolicy = policy
		}
	}
	scope, _ := domain.ParseCompactionHookScope("auto")
	declaration, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "observe", scope, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot([]domain.HookDeclaration{declaration}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := postCompactTestRunner{run: func(_ context.Context, id domain.HookRunIdentity) domain.HookRunResult {
		_, scope, _ := id.Target.Compaction()
		if scope.SampleIndex != 1 || conversation.outputs.finalized.Load() != 1 {
			t.Fatal("post hook entered before the tool output checkpoint")
		}
		return domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
	}}
	if err := runtime.InstallHooks(snapshot, runner, t.TempDir(), domain.HookOK); err != nil {
		t.Fatal(err)
	}
	runtime.interactiveApprovals = true
	correction, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "pending correction")
	control := &stopInputFixture{inputs: []domain.UserInput{correction}}
	trust := hookTrustEmitter(t, runtime, protocol.HookTrustSession, nil)
	err = runtime.runTurn(context.Background(), runtimeInputs("start"), runtime.profile, control, trust)
	if err == nil || !runtime.poisoned.Load() || runtime.autoCompactionFailures != 0 || conversation.calls.Load() != 1 ||
		!conversation.installed.Load() || len(control.states) != 2 || control.states[1] != protocol.InputReturned {
		t.Fatalf("tool continuation was not safely stopped: %v, samples=%d, input states=%v", err, conversation.calls.Load(), control.states)
	}
}

func (postCompactTestRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }
func (runner postCompactTestRunner) Run(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	return domain.HookExecution{Result: runner.run(ctx, id)}
}

func TestPostCompactAutomaticCompletionAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name      string
		cancelOn  session.EventKind
		failAt    int
		uncertain bool
		exhausted bool
		runs      int
		state     protocol.PostCompactStatus
	}{
		{name: "cancel as checkpoint syncs", cancelOn: session.EventCompactionCompleted, state: protocol.PostCompactCancelled},
		{name: "cancel as first admission syncs", cancelOn: session.EventPostCompactHookStarted, runs: 1, state: protocol.PostCompactCancelled},
		{name: "cancel as last result syncs", cancelOn: session.EventPostCompactHookFinished, runs: 2, state: protocol.PostCompactFailed},
		{name: "close sync fails", failAt: 8, runs: 2, state: protocol.PostCompactUncertain},
		{name: "runner cleanup uncertain", uncertain: true, runs: 1, state: protocol.PostCompactUncertain},
		{name: "execution budget leaves unstarted suffix", exhausted: true, runs: 1, state: protocol.PostCompactFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conversation := newCompactionConversation()
			runs := 0
			runner := postCompactTestRunner{run: func(ctx context.Context, id domain.HookRunIdentity) domain.HookRunResult {
				runs++
				if !conversation.installed.Load() {
					t.Fatal("runner entered before finalizing checkpoint")
				}
				result := domain.HookRunResult{Identity: id, Origin: "execution", Status: domain.HookError, Code: domain.HookExitFailed, ExitKnown: true, ExitCode: 2, CleanupConfirmed: true}
				if test.uncertain {
					return domain.HookRunResult{Identity: id, Origin: "execution", Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain}
				}
				if ctx.Err() != nil {
					return domain.HookRunResult{Identity: id, Origin: "execution", Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true}
				}
				return result
			}}
			journal := &fakeJournal{failAt: test.failAt, hook: func(drafts []session.RecordDraft) {
				if drafts[0].EventKind() == test.cancelOn &&
					(test.cancelOn != session.EventPostCompactHookFinished || runs == 2) {
					cancel()
				}
			}}
			runtime := newTestRuntime(t, conversation, journal)
			runtime.interactiveApprovals = true
			runtime.budget, _ = contextplan.NewBudget(60000, 0, 0)
			runtime.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, runtime.budget, 4096)
			runtime.autoCompactionFailures = 1
			scope, _ := domain.ParseCompactionHookScope("auto")
			declarations := make([]domain.HookDeclaration, 0, 2)
			for _, name := range []string{"first", "second"} {
				declaration, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest([]byte("fixture")), name, scope, "fixture", 5)
				if err != nil {
					t.Fatal(err)
				}
				declarations = append(declarations, declaration)
			}
			snapshot, err := domain.NewHookSnapshot(declarations, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.InstallHooks(snapshot, runner, t.TempDir(), domain.HookOK); err != nil {
				t.Fatal(err)
			}
			if test.exhausted {
				runtime.hooks.now = func() time.Time { return time.Unix(int64(runs)*30, 0) }
				conversation.stream = fixedStream(completedProviderEventWithSample(t, newPreparedSample(t, func() {})))
			}
			for _, declaration := range declarations {
				runtime.hooks.trust[declaration.Reference.Key()] = protocol.HookTrustSession
			}
			var closed protocol.PostCompactClosedPayload
			completions := 0
			err = runtime.runTurn(ctx, runtimeInputs("pending input"), runtime.profile, nil, func(event protocol.Event) {
				switch event.Kind {
				case protocol.EventPostCompactClosed:
					closed, err = protocol.DecodePostCompactClosed(event)
					if err != nil {
						t.Fatal(err)
					}
				case protocol.EventCompactionCompleted:
					completions++
				case protocol.EventCompactionFailed, protocol.EventHookTrustRequested:
					t.Fatalf("unexpected event: %s", event.Kind)
				}
			})
			wantPoison := test.state == protocol.PostCompactUncertain
			if (err == nil) != test.exhausted || errors.Is(err, context.Canceled) != (!wantPoison && !test.exhausted) ||
				runtime.poisoned.Load() != wantPoison || runtime.autoCompactionFailures != 0 ||
				closed.State != test.state || (closed.CloseSequence == 0) != (test.failAt != 0) ||
				runs != test.runs || completions != 1 || !conversation.installed.Load() {
				t.Fatalf("err=%v status=%+v runs=%d completions=%d failures=%d", err, closed, runs, completions, runtime.autoCompactionFailures)
			}
			if test.exhausted {
				if closed.Code != domain.HookExecutionBudgetExhausted || closed.Finished != 1 || conversation.calls.Load() != 1 {
					t.Fatal("known budget exhaustion did not resume sampling with the saved prefix")
				}
				return
			}
			if conversation.calls.Load() != 0 {
				t.Fatal("automatic cancellation or poison reached ordinary sampling")
			}
			for _, batch := range journal.snapshot() {
				if batch[0].EventKind() == session.EventTurnInputsCommitted {
					t.Fatal("automatic cancellation or poison committed the pending input")
				}
			}
		})
	}
}
