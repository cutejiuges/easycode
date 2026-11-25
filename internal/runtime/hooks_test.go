package runtime

import (
	"context"
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
func (f *fakeHookRunner) Run(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, timeout time.Duration) domain.HookExecution {
	f.runs++
	f.timeouts = append(f.timeouts, timeout)
	if f.onRun != nil {
		return domain.HookExecution{Result: f.onRun(ctx, id)}
	}
	status, code := f.status, f.code
	if status == "" {
		status, code = domain.HookContinue, domain.HookOK
	}
	r := domain.HookRunResult{Identity: id, Status: status, Code: code, CleanupConfirmed: true, Origin: "execution", ExitKnown: true}
	if status == domain.HookBlock {
		r.ExitCode = 2
	}
	return domain.HookExecution{Result: r}
}
func installRuntimeTestHooks(t *testing.T, runtime *Runtime, runner HookRunner, cwd string, declarations []domain.HookDeclaration) {
	t.Helper()
	snapshot, err := domain.NewHookSnapshot(declarations, nil)
	requireNoError(t, err)
	if err := runtime.InstallHooks(snapshot, runner, cwd, domain.HookOK); err != nil {
		t.Fatal(err)
	}
}

func hookRuntimeFixture(t *testing.T, journal Journal, count int) (*Runtime, tool.Invocation, *fakeHookRunner, []domain.HookDeclaration) {
	t.Helper()
	r := newTestRuntime(t, &fakeToolConversation{fakeConversation: &fakeConversation{}}, journal)
	r.interactiveApprovals = true
	inv, _ := runtimePatchInvocation(t)
	var declarations []domain.HookDeclaration
	for i := range count {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check-"+string(rune('a'+i)), 7, "cat >/dev/null; printf '{}'", 5)
		requireNoError(t, err)
		declarations = append(declarations, d)
	}
	runner := &fakeHookRunner{}
	installRuntimeTestHooks(t, r, runner, "/workspace", declarations)
	seedRuntimeToolCall(t, r, runtimeTurnID, inv, 0, 0)
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
		requireNoError(t, err)
		response, err := protocol.NewHookTrustResponse(r.sessionID, r.threadID, e.TurnID, p, decision)
		requireNoError(t, err)
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
			r.tools.PreparePatch, r.tools.Patch = preparer, executor
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

func requireNoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
