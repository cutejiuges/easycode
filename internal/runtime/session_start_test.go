package runtime

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestSessionStartCompactCancellationFollowsWriterAdmission(t *testing.T) {
	for _, test := range []struct {
		kind     session.EventKind
		wantRuns int
		facts    []session.EventKind
	}{
		{kind: session.EventCompactionCompleted},
		{kind: session.EventSessionStartHookPlanned, facts: []session.EventKind{session.EventSessionStartHookPlanned, session.EventSessionStartHookClosed}},
		{kind: session.EventSessionStartHookStarted, wantRuns: 1, facts: []session.EventKind{session.EventSessionStartHookPlanned, session.EventSessionStartHookStarted, session.EventSessionStartHookFinished, session.EventSessionStartHookClosed}},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conversation := newCompactionConversation()
			journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
				if drafts[0].EventKind() == test.kind {
					cancel()
				}
			}}
			r := newTestRuntime(t, conversation, journal)
			runs := 0
			runner := postCompactTestRunner{run: func(ctx context.Context, identity domain.HookRunIdentity) domain.HookRunResult {
				runs++
				if !conversation.installed.Load() || ctx.Err() == nil {
					t.Fatal("runner did not receive the cancelled admitted activity")
				}
				return domain.HookRunResult{Identity: identity, Status: domain.HookCancel, Code: domain.HookCancelled, Origin: "execution", Cancelled: true, CleanupConfirmed: true}
			}}
			snapshot := sessionStartTestSnapshot(t, "compact", "fixture")
			installSessionStartTestHooks(t, r, snapshot, runner)
			admission, code := r.preflightCompaction("")
			if code != "" {
				t.Fatal(code)
			}
			id, err := domain.GenerateCompactionID()
			if err != nil {
				t.Fatal(err)
			}
			complete, cancelled, reviews := 0, 0, 0
			trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
			err = r.runCompaction(ctx, id, admission, func(event protocol.Event) {
				if event.Kind == protocol.EventCompactionCompleted {
					complete++
				}
				if event.Kind == protocol.EventHookTrustRequested {
					reviews++
					trust(event)
				}
				if event.Kind == protocol.EventSessionStart {
					payload, err := protocol.DecodeSessionStart(event)
					if err != nil {
						t.Fatal(err)
					}
					if payload.State == protocol.SessionStartCancelled {
						cancelled++
					}
				}
			})
			var facts []session.EventKind
			for _, batch := range journal.snapshot() {
				switch batch[0].EventKind() {
				case session.EventSessionStartHookPlanned, session.EventSessionStartHookStarted, session.EventSessionStartHookFinished, session.EventSessionStartHookClosed:
					facts = append(facts, batch[0].EventKind())
				}
			}
			wantClosed := 0
			if len(test.facts) > 0 {
				wantClosed = 1
			}
			if !errors.Is(err, context.Canceled) || !conversation.installed.Load() || r.poisoned.Load() || complete != 1 || cancelled != wantClosed || reviews != test.wantRuns || runs != test.wantRuns || !slices.Equal(facts, test.facts) {
				t.Fatalf("cancel err=%v applied=%v completed=%d cancelled=%d reviews=%d runs=%d facts=%v", err, conversation.installed.Load(), complete, cancelled, reviews, runs, facts)
			}
		})
	}
}

func TestSessionStartRespectsAllTrustAndSharedDeadline(t *testing.T) {
	for _, denySecond := range []bool{false, true} {
		conversation := newCompactionConversation()
		r := newTestRuntime(t, conversation, &fakeJournal{})
		reviews, runs := 0, 0
		runner := postCompactTestRunner{run: func(_ context.Context, identity domain.HookRunIdentity) domain.HookRunResult {
			runs++
			if reviews != 2 || identity.Ordinal != 0 {
				t.Fatal("execution bypassed whole-plan trust or cumulative budget")
			}
			return domain.HookRunResult{Identity: identity, Status: domain.HookContinue, Code: domain.HookOK, Origin: "execution", ExitKnown: true, CleanupConfirmed: true}
		}}
		snapshot := sessionStartTestSnapshot(t, "startup", "first", "second")
		installSessionStartTestHooks(t, r, snapshot, runner)
		r.hooks.now = func() time.Time { return time.Unix(int64(runs)*30, 0) }
		turn, err := domain.GenerateTurnID()
		if err != nil {
			t.Fatal(err)
		}
		selection := r.selectSessionStart(domain.SessionStartStartup)
		attempt, err := r.runSessionStart(context.Background(), domain.SessionStartTarget{Source: domain.SessionStartStartup, TurnID: turn}, selection, func(event protocol.Event) {
			if event.Kind != protocol.EventHookTrustRequested {
				return
			}
			reviews++
			decision := protocol.HookTrustSession
			if denySecond && reviews == 2 {
				decision = protocol.HookTrustDeny
			}
			hookTrustEmitter(t, r, decision, nil)(event)
		})
		want, wantRuns := domain.HookExecutionBudgetExhausted, 1
		if denySecond {
			want, wantRuns = domain.HookTrustDenied, 0
		}
		if err != nil || reviews != 2 || runs != wantRuns || attempt.finished != wantRuns || attempt.code != want {
			t.Fatalf("deny=%v reviews=%d runs=%d code=%s err=%v", denySecond, reviews, runs, attempt.code, err)
		}
	}
}

func sessionStartTestSnapshot(t *testing.T, source string, names ...string) domain.HookSnapshot {
	t.Helper()
	scope, err := domain.ParseSessionStartHookScope(source)
	if err != nil {
		t.Fatal(err)
	}
	declarations := make([]domain.HookDeclaration, 0, len(names))
	for _, name := range names {
		declaration, err := domain.NewSessionStartHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest([]byte("fixture")), name, scope, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, declaration)
	}
	snapshot, err := domain.NewHookSnapshot(declarations, nil)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func installSessionStartTestHooks(t *testing.T, r *Runtime, snapshot domain.HookSnapshot, runner HookRunner) {
	t.Helper()
	r.interactiveApprovals = true
	if err := r.InstallHooks(snapshot, runner, t.TempDir(), domain.HookOK); err != nil {
		t.Fatal(err)
	}
}

func TestSessionStartAppliedSyncDeterminesInitialInputDisposition(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		instance, err := openai.New(openai.Config{Model: "gpt-test", APIKey: secret.New("fixture-key"), BaseURL: "http://127.0.0.1:1"})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = instance.Close() })
		conversation, err := instance.NewConversation(runtimeThreadID)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
			if len(drafts) == 3 && drafts[0].EventKind() == session.EventSessionStartHookClosed {
				cancel()
			}
		}}
		if unknown {
			journal.failAt = 5
		}
		r := newTestRuntime(t, conversation, journal)
		scope, _ := domain.ParseSessionStartHookScope("startup")
		declaration, err := domain.NewSessionStartHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "fixture", scope, "true", 5)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		snapshot, err := domain.NewHookSnapshot([]domain.HookDeclaration{declaration}, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		installSessionStartTestHooks(t, r, snapshot, sessionStartFeedbackRunner("frozen context"))
		if err := r.InstallSessionStartSource(domain.SessionStartStartup); err != nil {
			cancel()
			t.Fatal(err)
		}
		control := &stopInputFixture{}
		var applied int
		trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
		err = r.runTurn(ctx, runtimeInputs("original"), r.profile, control, func(event protocol.Event) {
			trust(event)
			if event.Kind == protocol.EventSessionStart {
				value, err := protocol.DecodeSessionStart(event)
				if err == nil && value.State == protocol.SessionStartApplied {
					applied++
				}
			}
		})
		cancel()
		wantDisposition, wantApplied, wantHistory := protocol.InputCommitted, 1, 1
		if unknown {
			wantDisposition, wantApplied, wantHistory = protocol.InputUncertain, 0, 0
		}
		if err == nil || !slices.Equal(control.states, []protocol.InputState{wantDisposition}) || applied != wantApplied || r.poisoned.Load() != unknown || len(conversation.ProjectHistory().Turns) != wantHistory {
			t.Fatalf("unknown=%v disposition=%v applied=%d history=%d err=%v", unknown, control.states, applied, len(conversation.ProjectHistory().Turns), err)
		}
	}
}

type sessionStartFeedbackRunner string

func (sessionStartFeedbackRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }

func (runner sessionStartFeedbackRunner) Run(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	feedback, _ := domain.NewHookFeedbackCandidate(string(runner))
	return domain.HookExecution{
		Result:   domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, Origin: "execution", ExitKnown: true, CleanupConfirmed: true},
		Feedback: feedback,
	}
}

func TestSessionStartRetainsActivationOnInputPreflightAndOmitsOverBudgetContext(t *testing.T) {
	instance, err := openai.New(openai.Config{Model: "gpt-test", APIKey: secret.New("fixture-key"), BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation, err := instance.NewConversation(runtimeThreadID)
	if err != nil {
		t.Fatal(err)
	}
	journal := &fakeJournal{}
	r := newTestRuntime(t, conversation, journal)
	scope, _ := domain.ParseSessionStartHookScope("resume")
	declaration, err := domain.NewSessionStartHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "fixture", scope, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot([]domain.HookDeclaration{declaration}, nil)
	if err != nil {
		t.Fatal(err)
	}
	installSessionStartTestHooks(t, r, snapshot, sessionStartFeedbackRunner(strings.Repeat("optional context ", 128)))
	if err := r.InstallSessionStartSource(domain.SessionStartResume); err != nil {
		t.Fatal(err)
	}
	inputs := runtimeInputs("exact original input")
	a, _ := inputAssociation(runtimeTurnID, 0, inputs)
	plain, err := r.providerOperations.userInputs.PrepareUserInputs(a, inputs)
	if err != nil {
		t.Fatal(err)
	}
	view, footprint, err := plain.Preview()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := r.planWindow(view, footprint)
	if err != nil {
		t.Fatal(err)
	}
	tokens, ok := plan.TotalEstimate().Tokens()
	if err := plain.Discard(); !ok || err != nil {
		t.Fatal("failed to measure plain input", err)
	}
	r.budget, err = contextplan.NewBudget(tokens+1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	denied := r.commitInitialInputs(ctx, runtimeTurnID, runtimeInputs(strings.Repeat("oversized first attempt ", 2000)), nil, func(event protocol.Event) {
		t.Fatal("a locally rejected input dispatched activity", event.Kind)
	})
	if !denied.preflightRejected || r.sessionStartSource != domain.SessionStartResume || len(journal.snapshot()) != 0 {
		t.Fatal("input rejection consumed or dispatched the activation", denied.err)
	}
	var skipped bool
	trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
	result := r.commitInitialInputs(ctx, runtimeTurnID, inputs, nil, func(event protocol.Event) {
		trust(event)
		if event.Kind == protocol.EventSessionStart {
			value, err := protocol.DecodeSessionStart(event)
			if err == nil && value.State == protocol.SessionStartSkipped && value.Code == domain.HookFeedbackBudgetExceeded {
				skipped = true
			}
		}
	})
	history := conversation.ProjectHistory()
	if result.err != nil || !result.committed || !skipped || r.sessionStartSource != "" || len(history.Turns) != 1 ||
		len(history.Turns[0].Messages) != 1 || history.Turns[0].Messages[0].Text != inputs[0].Text() {
		t.Fatalf("budget fallback lost the original input or admitted extra context: %+v", result)
	}
	batches := journal.snapshot()
	if len(batches) != 5 || len(batches[3]) != 1 || batches[3][0].EventKind() != session.EventSessionStartHookClosed || len(batches[4]) != 2 || batches[4][0].EventKind() != session.EventTurnInputsCommitted {
		t.Fatal("conditional fallback did not retain the original batch")
	}
}

func TestSessionStartManualCancellationReturnsAutoAndRetainsStrictNext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conversation := newCompactionConversation()
	conversation.stream = fixedStream(completedProviderEvent(t))
	r := newTestRuntime(t, conversation, &fakeJournal{})
	installSessionStartTestHooks(t, r, sessionStartTestSnapshot(t, "compact", "fixture"), postCompactTestRunner{run: func(context.Context, domain.HookRunIdentity) domain.HookRunResult {
		t.Error("cancelled review dispatched a command")
		return domain.HookRunResult{}
	}})
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	select {
	case <-loop.Ready():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	submit := func(command protocol.Command, err error) protocol.CommandResult {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		result, err := loop.Submit(ctx, command)
		if err != nil || result.Status() != protocol.CommandStatusAccepted {
			t.Fatal("not accepted", result.Status(), err)
		}
		return result
	}
	compaction := submit(protocol.NewCompactContextCommand("compact", ""))
	for {
		select {
		case item, ok := <-loop.Outputs():
			if !ok {
				t.Fatal("control stopped before review")
			}
			event, _ := item.ContextEvent()
			if event.Kind != protocol.EventHookTrustRequested {
				continue
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		break
	}
	submit(protocol.NewSubmitConversationalInputCommand("queued-auto", "return me"))
	submit(protocol.NewSubmitInputCommand("strict-next", "keep me"))
	submit(protocol.NewCancelCompactionCommand("cancel", compaction.CompactionID()))
	if err := loop.CloseInput(ctx); err != nil {
		t.Fatal(err)
	}
	var order []string
	for item := range loop.Outputs() {
		if event, _ := item.ContextEvent(); event.Kind == protocol.EventCompactionCompleted {
			order = append(order, "checkpoint")
		}
		if status, ok := item.InputStatus(); ok && status.State() == protocol.InputReturned {
			if status.RequestID() != "queued-auto" || status.Reason() != protocol.InputReasonCancelled {
				t.Fatal("wrong returned input", status.RequestID(), status.Reason())
			}
			order = append(order, "returned")
		}
		if event, _ := item.TurnEvent(); event.Kind == protocol.EventTurnCompleted {
			order = append(order, string(item.RequestID()))
		}
	}
	if err := <-done; err != nil || !slices.Equal(order, []string{"checkpoint", "returned", "strict-next"}) || conversation.calls.Load() != 1 {
		t.Fatalf("unexpected queued outcome %v, calls %d, error %v", order, conversation.calls.Load(), err)
	}
}
