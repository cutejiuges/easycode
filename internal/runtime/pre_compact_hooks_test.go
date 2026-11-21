package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

type preCompactTestRunner struct {
	preflights int
	runs       int
	focus      string
	uncertain  bool
	blockAt    int
}

func (runner *preCompactTestRunner) Preflight(context.Context) domain.HookCode {
	runner.preflights++
	return domain.HookOK
}

func (runner *preCompactTestRunner) Run(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	runner.runs++
	if runner.uncertain {
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}}
	}
	if runner.runs == runner.blockAt {
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, Origin: "execution", ExitKnown: true, CleanupConfirmed: true, StdoutBytes: 18}}
	}
	feedback, _ := domain.NewHookFeedbackCandidate(runner.focus)
	return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, Origin: "execution", ExitKnown: true, CleanupConfirmed: true, StdoutBytes: int64(len(runner.focus))}, Feedback: feedback}
}

func TestPreCompactReviewsAllAndStartsOnlyContinuingPrefix(t *testing.T) {
	for _, denyTrust := range []bool{true, false} {
		conversation := newCompactionConversation()
		runner := &preCompactTestRunner{blockAt: 2}
		runtime := newTestRuntime(t, conversation, &fakeJournal{})
		first := installPreCompactFixture(t, runtime, runner, true)
		declarations := []domain.HookDeclaration{first}
		for _, id := range []string{"second", "third"} {
			declaration, err := domain.NewPreCompactHookDeclaration(first.Reference.Source, first.Reference.Feedback, first.Reference.Revision, id, first.Scope, first.Command, first.Reference.TimeoutSeconds)
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
		admission, code := runtime.preflightCompaction("")
		if code != "" {
			t.Fatal(code)
		}
		ctx, cancel := context.WithCancel(context.Background())
		var reviews int
		var terminal protocol.Event
		id, _ := domain.GenerateCompactionID()
		err = runtime.runCompaction(ctx, id, admission, func(event protocol.Event) {
			terminal = event
			if event.Kind != protocol.EventHookTrustRequested {
				return
			}
			if runner.runs != 0 {
				t.Error("runner entered before all reviews")
			}
			reviews++
			request, err := protocol.DecodeHookTrustRequested(event)
			choice := protocol.HookTrustSession
			if denyTrust && reviews == 2 {
				choice = protocol.HookTrustDeny
			}
			response, responseErr := protocol.NewHookTrustResponse(runtimeSessionID, runtimeThreadID, "", request, choice)
			if err != nil || responseErr != nil || runtime.ResolveHookTrust(response) != nil {
				cancel()
				t.Errorf("review did not match request: %v %v", err, responseErr)
			}
		})
		cancel()
		failure, decodeErr := protocol.DecodeCompactionFailed(terminal)
		wantCode, wantReviews, wantRuns := protocol.CompactionFailureHookDenied, 3, 2
		if denyTrust {
			wantCode, wantReviews, wantRuns = protocol.CompactionFailureHookTrustDenied, 2, 0
		}
		if err == nil || decodeErr != nil || !failure.Durable || failure.Code != wantCode || reviews != wantReviews || runner.runs != wantRuns || conversation.generated.Load() != 0 {
			t.Fatalf("deny=%t outcome=%+v reviews=%d runs=%d errors=%v/%v", denyTrust, failure, reviews, runner.runs, err, decodeErr)
		}
	}
}

func installPreCompactFixture(t *testing.T, runtime *Runtime, runner HookRunner, interactive bool) domain.HookDeclaration {
	t.Helper()
	scope, err := domain.ParsePreCompactHookScope("*")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := domain.NewPreCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest([]byte("compact-fixture")), "check", scope, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot([]domain.HookDeclaration{declaration}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallHooks(snapshot, runner, t.TempDir(), domain.HookOK); err != nil {
		t.Fatal(err)
	}
	runtime.interactiveApprovals = interactive
	return declaration
}

func TestPreCompactSyncSeparatesPermissionFromNetworkAndCheckpoint(t *testing.T) {
	for _, test := range []struct {
		name      string
		cancelOn  session.EventKind
		failAt    int
		uncertain bool
		want      protocol.CompactionFailureCode
	}{
		{name: "cancel while gate Sync completes", cancelOn: session.EventPreCompactHookDecided, want: protocol.CompactionFailureCancelled},
		{name: "cancel while checkpoint Sync completes", cancelOn: session.EventCompactionCompleted},
		{name: "decision Sync uncertainty", failAt: 4, want: protocol.CompactionFailureSession},
		{name: "runner cleanup uncertainty", uncertain: true, want: protocol.CompactionFailureSession},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conversation := newCompactionConversation()
			runner := &preCompactTestRunner{focus: "test focus", uncertain: test.uncertain}
			journal := &fakeJournal{failAt: test.failAt, hook: func(drafts []session.RecordDraft) {
				kind := drafts[0].EventKind()
				if kind == session.EventPreCompactHookStarted && runner.runs != 0 {
					t.Error("runner started before its journal admission")
				}
				if kind == session.EventPreCompactHookDecided && conversation.generated.Load() != 0 {
					t.Error("network ran before its journal admission")
				}
				if kind == test.cancelOn {
					cancel()
				}
			}}
			runtime := newTestRuntime(t, conversation, journal)
			declaration := installPreCompactFixture(t, runtime, runner, true)
			runtime.hooks.trust[declaration.Reference.Key()] = protocol.HookTrustSession
			admission, code := runtime.preflightCompaction("user focus")
			if code != "" {
				t.Fatal(code)
			}
			var terminal protocol.Event
			id, _ := domain.GenerateCompactionID()
			err := runtime.runCompaction(ctx, id, admission, func(event protocol.Event) { terminal = event })
			if test.want == "" {
				if err != nil || !conversation.installed.Load() || terminal.Kind != protocol.EventCompactionCompleted || conversation.generated.Load() != 1 {
					t.Fatalf("durable checkpoint lost after cancellation: %v", err)
				}
				return
			}
			failure, decodeErr := protocol.DecodeCompactionFailed(terminal)
			if err == nil || decodeErr != nil || failure.Code != test.want || failure.Durable != (test.failAt == 0) || failure.UsageState != "unavailable" || conversation.generated.Load() != 0 || conversation.installed.Load() {
				t.Fatalf("unexpected failure: %+v, %v, %v", failure, decodeErr, err)
			}
			if runtime.poisoned.Load() != (test.want == protocol.CompactionFailureSession) {
				t.Fatal("wrong acceptance state after failure")
			}
		})
	}
}

func TestPreCompactRejectsBeforeNetwork(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		conversation := newCompactionConversation()
		runner := &preCompactTestRunner{focus: strings.Repeat("h", domain.MaxHookFeedbackBytes)}
		journal := &fakeJournal{}
		runtime := newTestRuntime(t, conversation, journal)
		declaration := installPreCompactFixture(t, runtime, runner, interactive)
		runtime.hooks.trust[declaration.Reference.Key()] = protocol.HookTrustSession
		admission, code := runtime.preflightCompaction(strings.Repeat("u", domain.MaxCompactionInstructionsBytes))
		if code != "" {
			t.Fatal(code)
		}
		var terminal protocol.Event
		id, _ := domain.GenerateCompactionID()
		err := runtime.runCompaction(context.Background(), id, admission, func(event protocol.Event) { terminal = event })
		failure, decodeErr := protocol.DecodeCompactionFailed(terminal)
		wantCode, wantRuns := protocol.CompactionFailureHookTrustRequired, 0
		if interactive {
			wantCode, wantRuns = protocol.CompactionFailureBudget, 1
		}
		if err == nil || decodeErr != nil || failure.Code != wantCode || !failure.Durable || failure.UsageState != "unavailable" || conversation.generated.Load() != 0 || runner.runs != wantRuns || runner.preflights != wantRuns {
			t.Fatalf("interactive=%t terminal=%+v runs=%d preflights=%d decode=%v err=%v", interactive, failure, runner.runs, runner.preflights, decodeErr, err)
		}
	}
}

func TestAutoCompactionRoutingRetainsCurrentIdentityThroughPresentation(t *testing.T) {
	scope, _ := domain.NewAutoCompactionScope(runtimeTurnID, 0)
	id, _ := domain.GenerateCompactionID()
	turn := &activeLoopTurn{turnID: runtimeTurnID}
	started, _ := protocol.NewCompactionStarted(runtimeSessionID, runtimeThreadID, id, 2, scope)
	loop := newControlledAgentLoop(t, newCompactionConversation(), &fakeJournal{})
	state := &agentLoopRunState{loop: loop}
	wrongThread := started
	wrongThread.ThreadID = domain.ThreadID(id)
	if _, err := state.correlateCompactionEvent("fixture", wrongThread); err == nil {
		t.Fatal("valid event from a different thread was accepted")
	}
	if err := turn.acceptCompactionEvent(runtimePublication{event: started}); err != nil {
		t.Fatal(err)
	}
	summarizing, _ := protocol.NewCompactionSummarizing(runtimeSessionID, runtimeThreadID, id, scope)
	if err := turn.acceptCompactionEvent(runtimePublication{event: summarizing}); err != nil || turn.compactionID != id {
		t.Fatalf("presentation changed identity: %v", err)
	}
	failed, _ := protocol.NewCompactionFailed(runtimeSessionID, runtimeThreadID, id, protocol.CompactionFailedPayload{Scope: scope, Code: protocol.CompactionFailureCancelled, Durable: true, UsageState: "unavailable"})
	if err := turn.acceptCompactionEvent(runtimePublication{event: failed}); err != nil || turn.compactionID.Valid() {
		t.Fatalf("terminal failed to retire identity: %v", err)
	}
	if turn.acceptCompactionEvent(runtimePublication{event: summarizing}) == nil {
		t.Fatal("stale presentation accepted")
	}
	scope, _ = domain.NewAutoCompactionScope(runtimeTurnID, 1)
	id, _ = domain.GenerateCompactionID()
	failed, err := protocol.NewCompactionFailed(runtimeSessionID, runtimeThreadID, id, protocol.CompactionFailedPayload{Scope: scope, Code: protocol.CompactionFailureSession, UsageState: "unavailable"})
	if err != nil || turn.acceptCompactionEvent(runtimePublication{event: failed}) != nil || turn.compactionID.Valid() || !turn.autoCompactionSeen {
		t.Fatalf("predurable failure was not accepted: %v", err)
	}
	if err := turn.acceptCompactionEvent(runtimePublication{event: failed}); err == nil {
		t.Fatalf("duplicate predurable failure was accepted: %v", err)
	}
}
