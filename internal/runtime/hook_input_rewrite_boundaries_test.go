package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func singleRewriteRuntime(t *testing.T, j *fakeJournal, withPost bool) (*Runtime, tool.Invocation, tool.Invocation, *postTestRunner, func() int, func() int) {
	t.Helper()
	r, original, _, _ := hookRuntimeFixture(t, j, 0)
	input := rewrittenFixtureInput(t, original, false)
	effective, err := tool.RewriteInvocation(original, input)
	requireNoError(t, err)
	prepare, execute, _ := rewriteCandidateFixture(t, r, effective)
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), "rewrite", 7, "true", 5)
	ds := []domain.HookDeclaration{d}
	if withPost {
		for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
			post, _ := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, domain.HookDigest(nil), map[domain.HookEvent]string{domain.HookPostToolUse: "post", domain.HookPostToolUseFailure: "failure"}[event], 7, "true", 5)
			ds = append(ds, post)
		}
	}
	runner := &postTestRunner{run: func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, in domain.HookInput, _ time.Duration) domain.HookExecution {
		stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		if id.Event.Post() {
			return domain.HookExecution{Result: stats}
		}
		replacement, err := domain.NewHookInputReplacement(input, in.RewriteContext().Profile())
		requireNoError(t, err)
		return domain.HookExecution{Result: stats, Replacement: replacement}
	}}
	installRuntimeTestHooks(t, r, runner, "/workspace", ds)
	r.toolTurn.resultBudget = tool.NewResultBudget()
	if err := r.toolTurn.resultBudget.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	return r, original, effective, runner, prepare, execute
}

type failingPreHistory struct{ calls, failAt int }

func (h *failingPreHistory) PreHooksCommitted(tool.Invocation, uint64, session.ReplayedHookGate) error {
	h.calls++
	if h.calls == h.failAt {
		return errors.New("fixture host install failed")
	}
	return nil
}
func (*failingPreHistory) PostHooksCommitted(tool.InvocationID, uint64, session.ReplayedPostHooks) error {
	return nil
}

func TestPreRewriteDurableInstallAndSyncFailuresPoisonBeforeOrdinaryEffects(t *testing.T) {
	for _, hostFailure := range []bool{false, true} {
		for _, at := range []int{1, 2, 3} {
			t.Run(map[bool]string{true: "host", false: "sync"}[hostFailure]+string(rune('0'+at)), func(t *testing.T) {
				j := &fakeJournal{}
				r, original, _, runner, prepare, execute := singleRewriteRuntime(t, j, false)
				emissions := 0
				r.toolTurn.activityEmitter = func(e protocol.Event) {
					p, err := protocol.DecodeHookActivity(e)
					state := r.toolTurn.toolCalls[original.InvocationID()]
					if err != nil || state.pre == nil || state.detailSequence != p.PreInput.Sequence() || !state.original.Equal(original) {
						t.Fatal("activity preceded committed snapshot installation", err)
					}
					emissions++
				}
				if hostFailure {
					r.hookHistory = &failingPreHistory{failAt: at}
				} else {
					j.failAt = at
				}
				_, _, err := r.executeApproved(context.Background(), postApprovalEmitter(t, r, original, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, original)
				if err == nil || !r.poisoned.Load() || emissions != at-1 || prepare() != 0 || execute() != 0 || countRewriteFact(t, j, session.EventToolApprovalDecided) != 0 || countRewriteFact(t, j, session.EventToolExecutionStarted) != 0 {
					t.Fatal("failed install continued", err)
				}
				before, runs := len(j.snapshot()), runner.calls
				if _, _, err := r.executeApproved(context.Background(), nil, runtimeTurnID, original); err == nil || runner.calls != runs || len(j.snapshot()) != before {
					t.Fatal("poisoned rewrite reran")
				}
			})
		}
	}
}
