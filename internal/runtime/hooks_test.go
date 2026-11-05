package runtime

import (
	"context"
	contextplan "easycode/internal/context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type fakeHookRunner struct {
	runs, preflights int
	preflight        domain.HookCode
	status           domain.HookOutcome
	code             domain.HookCode
	onRun            func(context.Context, domain.HookRunIdentity) domain.HookRunResult
	timeouts         []time.Duration
}

func (f *fakeHookRunner) Preflight(context.Context) domain.HookCode {
	f.preflights++
	if f.preflight != "" {
		return f.preflight
	}
	return domain.HookOK
}
func (f *fakeHookRunner) Run(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, timeout time.Duration) domain.HookRunResult {
	f.runs++
	f.timeouts = append(f.timeouts, timeout)
	if f.onRun != nil {
		return f.onRun(ctx, id)
	}
	status, code := f.status, f.code
	if status == "" {
		status, code = domain.HookContinue, domain.HookOK
	}
	r := domain.HookRunResult{Identity: id, Status: status, Code: code, CleanupConfirmed: true, Origin: "execution", ExitKnown: true}
	if status == domain.HookBlock {
		r.ExitCode = 2
	}
	return r
}
func hookRuntimeFixture(t *testing.T, journal Journal, count int) (*Runtime, tool.Invocation, *fakeHookRunner, []domain.HookDeclaration) {
	t.Helper()
	r := newTestRuntime(t, &fakeToolConversation{fakeConversation: &fakeConversation{}}, journal)
	r.interactiveApprovals = true
	inv, _ := runtimePatchInvocation(t)
	var declarations []domain.HookDeclaration
	for i := range count {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, strings.Repeat("a", 64), "check-"+string(rune('a'+i)), 7, "cat >/dev/null; printf '{}'", 5)
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, d)
	}
	s, err := domain.NewHookSnapshot(declarations, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeHookRunner{}
	if err := r.InstallHooks(s, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	return r, inv, runner, declarations
}
func hookTrustEmitter(t *testing.T, r *Runtime, decision protocol.HookTrustDecision, count *int) Emitter {
	t.Helper()
	return func(e protocol.Event) {
		if e.Kind != protocol.EventHookTrustRequested {
			return
		}
		if count != nil {
			*count++
		}
		p, err := protocol.DecodeHookTrustRequested(e)
		if err != nil {
			t.Fatal(err)
		}
		response, err := protocol.NewHookTrustResponse(r.sessionID, r.threadID, e.TurnID, p, decision)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ResolveHookTrust(response); err != nil {
			t.Fatal(err)
		}
		if err := r.ResolveHookTrust(response); err == nil {
			t.Fatal("duplicate response accepted")
		}
	}
}
func TestHookGateKeepsTrustSeparateAndCachesDurableDenial(t *testing.T) {
	journal := &fakeJournal{}
	r, inv, runner, ds := hookRuntimeFixture(t, journal, 2)
	requests := 0
	runner.status, runner.code = domain.HookBlock, domain.HookDenied
	result, blocked, err := r.checkToolHooks(context.Background(), hookTrustEmitter(t, r, protocol.HookTrustSession, &requests), runtimeTurnID, inv)
	if err != nil || !blocked || result.Code() != string(domain.HookDenied) || result.Status() != tool.ResultDenied || runner.runs != 1 || requests != 1 {
		t.Fatalf("gate failed: %v %v %s", err, blocked, result.Code())
	}
	batches := journal.snapshot()
	last := batches[len(batches)-1]
	if len(last) != 3 || last[0].EventKind() != session.EventHookExecutionFinished || last[1].EventKind() != session.EventToolHookGateDecided || last[2].EventKind() != session.EventToolCallResult {
		t.Fatal("non-atomic denial")
	}
	before := len(batches)
	again, _, err := r.checkToolHooks(context.Background(), hookTrustEmitter(t, r, protocol.HookTrustSession, &requests), runtimeTurnID, inv)
	if err != nil || again.Preview().Text() != result.Preview().Text() || runner.runs != 1 || len(journal.snapshot()) != before {
		t.Fatal("durable denial reran")
	}
	entries := r.HookEntries()
	if entries[0].State != "trusted" || entries[1].State != "untrusted" {
		t.Fatal("trust broadened")
	}
	target, _ := protocol.NewHookTrustTarget(r.sessionID, r.threadID, ds[0].Reference)
	if err := r.ResetHookTrust(target); err != nil || r.HookEntries()[0].State != "untrusted" {
		t.Fatal("reset failed", err)
	}
	r.active.Store(true)
	if err := r.ResetHookTrust(target); err == nil {
		t.Fatal("active reset accepted")
	}
	r.active.Store(false)
}
func TestHookGateContinueStillRequiresOrdinaryApproval(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(map[bool]string{true: "allow", false: "deny"}[allow], func(t *testing.T) {
			journal := &fakeJournal{}
			r, inv, runner, _ := hookRuntimeFixture(t, journal, 2)
			_, candidate := runtimePatchInvocation(t)
			preparer := &fixedPatchPreparer{candidate: candidate}
			executor := &fixedPatchExecutor{}
			r.patchPreparer, r.patchExecutor = preparer, executor
			trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
			var events []protocol.Event
			decision := tool.ApprovalDeny
			if allow {
				decision = tool.ApprovalAllowOnce
			}
			emit := func(e protocol.Event) {
				trust(e)
				if e.Kind == protocol.EventToolApprovalRequested {
					p, _ := protocol.DecodeToolApprovalRequested(e)
					id, _ := tool.ParseApprovalRequestID(p.RequestID)
					response, _ := tool.NewApprovalResponse(id, runtimeTurnID, inv.InvocationID(), tool.CandidateFingerprint(p.Fingerprint), decision, "")
					if err := r.ResolveToolApproval(response); err != nil {
						t.Fatal(err)
					}
				}
				events = append(events, e)
			}
			result, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv)
			if err != nil || runner.runs != 2 || preparer.calls != 1 {
				t.Fatal("gate did not continue through ordinary approval", err)
			}
			if allow && (executor.calls != 1 || result.Status() != tool.ResultSuccess) {
				t.Fatal("approved tool not executed")
			}
			if !allow && (executor.calls != 0 || result.Status() != tool.ResultDenied) {
				t.Fatal("hook trust authorized tool")
			}
			kinds := journal.snapshot()
			gate, approval := -1, -1
			for i, batch := range kinds {
				for _, d := range batch {
					if d.EventKind() == session.EventToolHookGateDecided {
						gate = i
					}
					if d.EventKind() == session.EventToolApprovalDecided {
						approval = i
					}
				}
			}
			if gate < 0 || approval <= gate {
				t.Fatal("approval preceded gate")
			}
		})
	}
}
func TestHookGateHeadlessAndPreflightFailuresDoNotStartAnything(t *testing.T) {
	for _, mode := range []string{"headless", "invalid", "too-many", "cancelled", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			j := &fakeJournal{}
			n := 1
			if mode == "too-many" {
				n = 9
			}
			r, inv, runner, _ := hookRuntimeFixture(t, j, n)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := domain.HookTrustRequired
			switch mode {
			case "headless":
				r.interactiveApprovals = false
			case "invalid":
				s, _ := domain.NewHookSnapshot(nil, []domain.HookDiagnostic{{Source: domain.HookSourceUser, Code: domain.HookConfigInvalid}})
				r.hooks.snapshot = s
				want = domain.HookConfigInvalid
			case "too-many":
				want = domain.HookMatchLimit
			case "unsupported":
				runner.preflight = domain.HookUnsupported
				want = domain.HookUnsupported
			case "cancelled":
				cancel()
				want = domain.HookCancelled
			}
			result, blocked, err := r.checkToolHooks(ctx, func(protocol.Event) { t.Fatal("preflight prompted") }, runtimeTurnID, inv)
			wantPreflights := 0
			if mode == "unsupported" {
				wantPreflights = 1
			}
			if err != nil || !blocked || result.Code() != string(want) || runner.runs != 0 || runner.preflights != wantPreflights {
				t.Fatal("preflight side effect", err, result.Code())
			}
		})
	}
}
func TestHookGateTotalBudgetAndRunnerMismatch(t *testing.T) {
	for _, mode := range []string{"budget", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			j := &fakeJournal{}
			r, inv, runner, _ := hookRuntimeFixture(t, j, 2)
			clockCalls := 0
			r.hooks.now = func() time.Time {
				clockCalls++
				if clockCalls == 1 {
					return time.Unix(0, 0)
				}
				return time.Unix(31, 0)
			}
			if mode == "mismatch" {
				runner.onRun = func(context.Context, domain.HookRunIdentity) domain.HookRunResult { return domain.HookRunResult{} }
			}
			result, blocked, err := r.checkToolHooks(context.Background(), hookTrustEmitter(t, r, protocol.HookTrustSession, nil), runtimeTurnID, inv)
			if !blocked || runner.runs != 1 {
				t.Fatal("invalid admission")
			}
			if mode == "budget" {
				if err != nil || result.Code() != string(domain.HookTimedOut) {
					t.Fatal("budget not enforced", err, result.Code())
				}
			} else if err == nil || result.Code() != string(domain.HookOutcomeUncertain) || !r.poisoned.Load() {
				t.Fatal("mismatch did not poison")
			}
		})
	}
}

type hookFaultJournal struct {
	fakeJournal
	kind    session.EventKind
	cancel  context.CancelFunc
	after   bool
	failure error
}

func (j *hookFaultJournal) AppendBatch(ctx context.Context, ds []session.RecordDraft) ([]session.Record, error) {
	match := false
	for _, d := range ds {
		match = match || d.EventKind() == j.kind
	}
	if match && !j.after {
		if j.cancel != nil {
			j.cancel()
		}
		if j.failure != nil {
			return nil, j.failure
		}
	}
	records, err := j.fakeJournal.AppendBatch(ctx, ds)
	if match && j.after {
		if j.cancel != nil {
			j.cancel()
		}
		if j.failure != nil {
			return records, j.failure
		}
	}
	return records, err
}
func TestHookGateSyncAndAcceptedCancellationBoundaries(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventToolHookGatePlanned, session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventToolHookGateDecided} {
		t.Run(string(kind), func(t *testing.T) {
			j := &hookFaultJournal{kind: kind, failure: errors.New("injected Sync failure")}
			r, inv, runner, _ := hookRuntimeFixture(t, j, 1)
			_, blocked, err := r.checkToolHooks(context.Background(), hookTrustEmitter(t, r, protocol.HookTrustSession, nil), runtimeTurnID, inv)
			want := 0
			if kind == session.EventHookExecutionFinished || kind == session.EventToolHookGateDecided {
				want = 1
			}
			if err == nil || !blocked || !r.poisoned.Load() || runner.runs != want {
				t.Fatal("Sync boundary violated", err, runner.runs)
			}
		})
	}
	for _, after := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		j := &hookFaultJournal{kind: session.EventHookExecutionStarted, cancel: cancel, after: after}
		if !after {
			j.failure = context.Canceled
		}
		r, inv, runner, _ := hookRuntimeFixture(t, j, 1)
		runner.onRun = func(ctx context.Context, id domain.HookRunIdentity) domain.HookRunResult {
			if ctx.Err() == nil {
				t.Fatal("cancelled context not delivered")
			}
			return domain.HookRunResult{Identity: id, Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true, Origin: "execution"}
		}
		result, _, err := r.checkToolHooks(ctx, hookTrustEmitter(t, r, protocol.HookTrustSession, nil), runtimeTurnID, inv)
		cancel()
		want := 0
		if after {
			want = 1
		}
		if err == nil || result.Status() != tool.ResultCancelled || runner.runs != want || r.poisoned.Load() {
			t.Fatal("cancel linearization violated", after, err, runner.runs)
		}
	}
}

func TestHookDenialPrecedesExistingMutationGrantAndCandidate(t *testing.T) {
	for _, capability := range []tool.CapabilityID{tool.CapabilityPatch, tool.CapabilityWrite} {
		t.Run(string(capability), func(t *testing.T) {
			r, inv, runner, _ := hookRuntimeFixture(t, &fakeJournal{}, 1)
			if capability == tool.CapabilityWrite {
				input, _ := tool.NewWriteInput("marker", "content")
				ready, _ := tool.NewWriteReadyCall("call-write", input)
				inv, _ = tool.NewInvocation(inv.InvocationID(), ready)
			}
			r.approvals.patchGrant.Store(true)
			r.approvals.writeGrant.Store(true)
			runner.status, runner.code = domain.HookBlock, domain.HookDenied
			result, _, err := r.executeApproved(context.Background(), hookTrustEmitter(t, r, protocol.HookTrustSession, nil), runtimeTurnID, inv)
			if err != nil || result.Status() != tool.ResultDenied || result.Code() != string(domain.HookDenied) || runner.runs != 1 {
				t.Fatal("grant bypassed hook", err)
			}
			for _, batch := range r.journal.(*fakeJournal).snapshot() {
				for _, d := range batch {
					if d.EventKind() == session.EventToolApprovalDecided || d.EventKind() == session.EventToolExecutionStarted {
						t.Fatal("candidate/approval reached after hook denial")
					}
				}
			}
		})
	}
}

func TestHookTrustWithoutNativeHistoryDoesNotChangeContextSegments(t *testing.T) {
	r, inv, _, _ := hookRuntimeFixture(t, &fakeJournal{}, 1)
	before, err := r.contextPlan()
	if err != nil {
		t.Fatal(err)
	}
	_, blocked, err := r.checkToolHooks(context.Background(), hookTrustEmitter(t, r, protocol.HookTrustSession, nil), runtimeTurnID, inv)
	if err != nil || blocked {
		t.Fatal(err)
	}
	after, err := r.contextPlan()
	if err != nil {
		t.Fatal(err)
	}
	first, err := contextplan.NewCacheSources(before)
	if err != nil {
		t.Fatal(err)
	}
	second, err := contextplan.NewCacheSources(after)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("hook trust or audit facts polluted stable/history segments")
	}
}
