package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

type promptTestRunner func(context.Context, domain.HookRunIdentity, domain.HookInput) domain.HookExecution

func (promptTestRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }

func (run promptTestRunner) Run(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
	return run(ctx, id, input)
}

func installPromptTestHook(t *testing.T, r *Runtime, runner HookRunner, additional ...domain.HookDeclaration) {
	t.Helper()
	d, err := domain.NewUserPromptSubmitHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "submit", "true", 5)
	requireNoError(t, err)
	r.interactiveApprovals = true
	installRuntimeTestHooks(t, r, runner, "/workspace", append(additional, d))
}

func TestPromptSubmitBlocksBeforeActivationAndStillAllowsNextInput(t *testing.T) {
	journal := &fakeJournal{}
	conversation := newCompactionConversation()
	conversation.footprint = conversation.HistoryFootprint
	conversation.stream = fixedStream(completedProviderEventWithSample(t, newPreparedSample(t, func() {})))
	r := newTestRuntime(t, conversation, journal)
	r.budget, _ = contextplan.NewBudget(60000, 0, 0)
	r.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, r.budget, 4096)
	r.newTurnID = sequentialTurnIDs()
	activationScope, _ := domain.ParseSessionStartHookScope("startup")
	activation, err := domain.NewSessionStartHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "activation", activationScope, "true", 5)
	requireNoError(t, err)
	var checked []domain.HookEvent
	runner := promptTestRunner(func(_ context.Context, id domain.HookRunIdentity, input domain.HookInput) domain.HookExecution {
		checked = append(checked, id.Event)
		if batches := journal.snapshot(); batches[len(batches)-1][0].EventKind() != session.EventHookExecutionStarted && id.Event == domain.HookUserPromptSubmit {
			t.Fatal("prompt command ran before its durable start")
		}
		result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		encoded, err := input.Bytes()
		requireNoError(t, err)
		if bytes.Contains(encoded, []byte("discarded-first")) {
			result.Status, result.Code, result.ExitCode = domain.HookBlock, domain.HookDenied, 2
		}
		return domain.HookExecution{Result: result}
	})
	installPromptTestHook(t, r, runner, activation)
	if err := r.InstallSessionStartSource(domain.SessionStartStartup); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
	if err := r.runFixtureTurn(ctx, runtimeInputs("discarded-first"), trust); !errors.Is(err, promptSubmitFailure(domain.HookDenied)) || conversation.generated.Load() != 0 || conversation.calls.Load() != 0 || r.sessionStartSource != domain.SessionStartStartup {
		t.Fatal("rejected input reached compaction, activation, or the provider", err)
	}
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if bytes.Contains(draft.PayloadBytes(), []byte("discarded-first")) {
				t.Fatal("rejected prompt escaped into the journal")
			}
		}
	}
	next, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "  allowed next\n")
	if err := r.runFixtureTurn(ctx, []domain.UserInput{next}, trust); err != nil || conversation.generated.Load() != 1 || conversation.calls.Load() != 1 || r.sessionStartSource != "" {
		t.Fatal("eligible input did not perform pending compaction and activation", err, conversation.generated.Load(), conversation.calls.Load())
	}
	if !slices.Equal(checked, []domain.HookEvent{domain.HookUserPromptSubmit, domain.HookUserPromptSubmit, domain.HookSessionStart}) {
		t.Fatal("unexpected hook order", checked)
	}
}

func TestPromptSubmitFiltersClaimWithoutDetachingTurnRequest(t *testing.T) {
	r := newTestRuntime(t, &fakeConversation{}, &fakeJournal{})
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	requireNoError(t, err)
	first := runtimeInputs("return first")[0]
	second, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "keep sibling")
	claim := []queuedInput{{value: first, requestID: "first", automatic: true}, {value: second, requestID: "sibling", automatic: true}}
	control := &loopInputControl{resolved: make(chan bool, 1)}
	state := agentLoopRunState{loop: loop, outstanding: map[protocol.RequestID]struct{}{"first": {}, "sibling": {}}, queuedBytes: first.ByteSize() + second.ByteSize()}
	state.active = &activeLoopTurn{turnID: runtimeTurnID, inputControl: control, input: claim[0], claim: claim}
	state.handleInputResolution(inputResolution{turnID: runtimeTurnID, control: control, inputs: []domain.UserInput{first, second}, rejected: []promptSubmitRejection{{code: domain.HookDenied}, {}}})
	if !<-control.resolved || len(state.active.claim) != 1 || state.active.claim[0].value.ID() != second.ID() || state.queuedBytes != second.ByteSize() || len(state.outstanding) != 2 {
		t.Fatal("partial return detached the owner or lost input quota")
	}
	item := <-loop.Outputs()
	status, _ := item.InputStatus()
	if status.EntryID() != first.ID() || status.State() != protocol.InputReturned || status.RequestID() != "first" {
		t.Fatal("wrong returned identity", status)
	}
	state.recent = newRecentRequestLedger(DefaultAgentLoopConfig().RecentRequests)
	state.handleInputResolution(inputResolution{turnID: runtimeTurnID, control: control, inputs: []domain.UserInput{second}, state: protocol.InputCommitted})
	if !<-control.resolved || state.queuedBytes != 0 || len(state.outstanding) != 1 || len(state.active.claim) != 0 {
		t.Fatal("retained input did not finish independently")
	}
}

func TestPromptSubmitUsesSharedBudgetAndRejectsLateTrust(t *testing.T) {
	r := newTestRuntime(t, &fakeConversation{}, &fakeJournal{})
	runner := &fakeHookRunner{}
	installPromptTestHook(t, r, runner)
	r.hooks.now = func() time.Time { return time.Unix(int64(runner.runs)*30, 0) }
	budget := 30 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := runtimeInputs("first input")
	_, _, err := r.checkPromptSubmissions(ctx, func(e protocol.Event) {
		if e.Kind != protocol.EventHookTrustRequested {
			return
		}
		p, _ := protocol.DecodeHookTrustRequested(e)
		response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, e.TurnID, p, protocol.HookTrustSession)
		cancel()
		if r.ResolveHookTrust(response) == nil {
			t.Fatal("accepted a late response to cancelled prompt check")
		}
	}, runtimeTurnID, 0, input, nil, &budget)
	if !errors.Is(err, context.Canceled) || runner.runs != 0 || r.HookEntries()[0].State != "untrusted" {
		t.Fatal("cancelled review changed trust or started a command", err)
	}
	trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
	if passed, _, err := r.checkPromptSubmissions(context.Background(), trust, runtimeTurnID, 0, input, nil, &budget); err != nil || len(passed) != 1 || budget != 0 {
		t.Fatal("first check did not consume the shared execution budget", err)
	}
	second, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "second input")
	if passed, code, err := r.checkPromptSubmissions(context.Background(), trust, runtimeTurnID, 1, []domain.UserInput{second}, nil, &budget); err != nil || len(passed) != 0 || code != domain.HookExecutionBudgetExhausted || runner.runs != 1 {
		t.Fatal("steering reset the prompt hook budget", err, code)
	}
}

func TestPromptSubmitRejectsUnsupportedPlatformBeforeDisclosure(t *testing.T) {
	j := &fakeJournal{}
	r, _, requests := newStopRuntimeFixture(t, j)
	runner := &fakeHookRunner{}
	installPromptTestHook(t, r, runner)
	r.hooks.availability = domain.HookUnsupported
	err := r.runFixtureTurn(context.Background(), runtimeInputs("undisclosed prompt"), func(event protocol.Event) {
		if event.Kind == protocol.EventHookTrustRequested || event.Kind == protocol.EventHookActivity {
			t.Fatal("unavailable platform reached interactive execution")
		}
	})
	if fault.Project(err).Code != fault.Code(domain.HookUnsupported) || requests.Load() != 0 || runner.preflights != 0 || runner.runs != 0 || j.Poisoned() {
		t.Fatal("unsupported platform did not settle locally", err, runner.preflights, runner.runs)
	}
}

func TestPromptSubmitChecksApprovedPlanOnceAsLiteralDefaultInput(t *testing.T) {
	for _, block := range []bool{false, true} {
		name := "continue"
		if block {
			name = "return"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conversation := &planReviewConversation{controlledConversation: newControlledConversation()}
			r := newControlledAgentLoop(t, conversation, &fakeJournal{}).runtime
			var payloads [][]byte
			runner := promptTestRunner(func(_ context.Context, id domain.HookRunIdentity, input domain.HookInput) domain.HookExecution {
				body, _ := input.Bytes()
				payloads = append(payloads, body)
				result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
				if block && len(payloads) == 2 {
					result.Status, result.Code, result.ExitCode = domain.HookBlock, domain.HookDenied, 2
				}
				return domain.HookExecution{Result: result}
			})
			installPromptTestHook(t, r, runner)
			host := openPlanReviewFixture(t, ctx, r)
			text, _ := domain.NewLiteralInputDraft("Plan the work")
			if result, err := host.Append(ctx, text); err != nil || !result.EntryID().Valid() {
				t.Fatal("plan input was not admitted", err)
			}
			var offer protocol.PlanReviewOffer
			for !offer.Ref.Sample.TurnID.Valid() {
				select {
				case turn := <-conversation.calls:
					turn.release <- completedProviderEvent(t)
				case item := <-host.Outputs():
					if e, ok := item.TurnEvent(); ok && e.Kind == protocol.EventHookTrustRequested {
						p, err := protocol.DecodeHookTrustRequested(e)
						requireNoError(t, err)
						if err := host.ResolveHookTrust(ctx, e.TurnID, p, protocol.HookTrustSession); err != nil {
							t.Fatal(err)
						}
					}
					if candidate, ok := item.PlanReviewOffer(); ok {
						offer = candidate
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			approved, err := host.ApprovePlan(ctx, "implementation", offer.Ref)
			if err != nil || !approved.EntryID().Valid() {
				t.Fatal("Plan approval was not admitted", err)
			}
			var confirmed, returned bool
			var sampled int
			for done := false; !done; {
				select {
				case turn := <-conversation.calls:
					sampled++
					if block || turn.input != domain.PlanImplementationInput(planReviewFixture) {
						t.Fatal("provider received an inadmissible or modified plan")
					}
					turn.release <- completedProviderEvent(t)
				case item := <-host.Outputs():
					if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventWorkModeSnapshot && item.RequestID() == "implementation" {
						mode, err := protocol.DecodeWorkModeSnapshot(event)
						confirmed = err == nil && mode.Mode == domain.WorkModeDefault
					}
					if status, ok := item.InputStatus(); ok && status.EntryID() == approved.EntryID() && status.State() == protocol.InputReturned {
						returned = true
					}
					if event, ok := item.TurnEvent(); ok && (event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed) {
						if block != (event.Kind == protocol.EventTurnFailed) {
							t.Fatal("wrong implementation terminal", event.Kind)
						}
						done = true
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var payload struct {
				InputID domain.InputID `json:"input_id"`
				Prompt  string         `json:"prompt"`
			}
			if !confirmed || returned != block || (!block && sampled != 1) || len(payloads) != 2 ||
				json.Unmarshal(payloads[1], &payload) != nil || payload.InputID != approved.EntryID() || payload.Prompt != domain.PlanImplementationInput(planReviewFixture) {
				t.Fatal("approved plan changed scope, original text, or input disposition", confirmed, returned, sampled, len(payloads))
			}
		})
	}
}

type promptWriteFaultJournal struct {
	fakeJournal
	intercept func(context.Context, []session.RecordDraft) ([]session.Record, error)
}

func (j *promptWriteFaultJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if drafts[0].EventKind() == session.EventTurnInputsCommitted {
		return j.intercept(ctx, drafts)
	}
	return j.fakeJournal.AppendBatch(ctx, drafts)
}

type promptResolutionFixture struct {
	inputResolutionRecorder
	inputs map[domain.InputID]protocol.InputState
}

func (f *promptResolutionFixture) filter(_ domain.TurnID, inputs []domain.UserInput, rejections []promptSubmitRejection) bool {
	for i, input := range inputs {
		if rejections[i].code != "" {
			f.inputs[input.ID()] = protocol.InputReturned
		}
	}
	return true
}

func (f *promptResolutionFixture) resolve(_ domain.TurnID, inputs []domain.UserInput, state protocol.InputState, _ protocol.InputReason) {
	for _, input := range inputs {
		f.inputs[input.ID()] = state
	}
}

func TestPromptSubmitSeparatesDenialsFromNativeWriterOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		want protocol.InputState
	}{
		{"cancel before native admission", protocol.InputReturned},
		{"cancel after native sync", protocol.InputCommitted},
		{"uncertain native sync", protocol.InputUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			j := &promptWriteFaultJournal{}
			j.intercept = func(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
				switch tc.want {
				case protocol.InputReturned:
					cancel()
					return nil, ctx.Err()
				case protocol.InputUncertain:
					j.mu.Lock()
					j.poisoned = true
					j.mu.Unlock()
					return nil, errors.New("fixture native write result unknown")
				}
				records, err := j.fakeJournal.AppendBatch(ctx, drafts)
				cancel()
				return records, err
			}
			r, _, requests := newStopRuntimeFixture(t, j)
			denied := runtimeInputs("deny before native write")[0]
			allowed, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "accepted for native write")
			runner := &fakeHookRunner{onRun: func(_ context.Context, id domain.HookRunIdentity) domain.HookRunResult {
				result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
				if target, _ := id.Target.UserPromptSubmit(); target.InputID == denied.ID() {
					result.Status, result.Code, result.ExitCode = domain.HookBlock, domain.HookDenied, 2
				}
				return result
			}}
			installPromptTestHook(t, r, runner)
			control := &promptResolutionFixture{inputs: make(map[domain.InputID]protocol.InputState)}
			err := r.runTurn(ctx, []domain.UserInput{denied, allowed}, r.profile, control, hookTrustEmitter(t, r, protocol.HookTrustSession, nil))
			if err == nil || control.inputs[denied.ID()] != protocol.InputReturned || control.inputs[allowed.ID()] != tc.want || requests.Load() != 0 {
				t.Fatal("check result and native write result were conflated", err, control.inputs, requests.Load())
			}
			if tc.want == protocol.InputUncertain && (fault.Project(err).Code != fault.CodeSessionWrite || !r.poisoned.Load()) {
				t.Fatal("uncertain native commit did not stop the session", err)
			}
		})
	}
}
