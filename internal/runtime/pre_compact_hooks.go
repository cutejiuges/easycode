package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type preCompactCheck struct {
	plan         session.PreCompactPlan
	declarations []domain.HookDeclaration
	runner       HookRunner
	workspace    string
	availability domain.HookCode
	now          func() time.Time
}

func (runtime *Runtime) planPreCompact(id domain.CompactionID, scope domain.CompactionScope, preview provider.CompactionPreview) (preCompactCheck, protocol.CompactionFailureCode, error) {
	runtime.hooks.mu.Lock()
	snapshot := runtime.hooks.snapshot
	check := preCompactCheck{runner: runtime.hooks.runner, workspace: runtime.hooks.workspace, availability: runtime.hooks.availability, now: runtime.hooks.now}
	runtime.hooks.mu.Unlock()
	declarations, err := snapshot.PreCompactChecks(scope.Trigger)
	if err != nil {
		cause := domain.HookConfigInvalid
		if errors.Is(err, domain.HookMatchLimit) {
			cause = domain.HookMatchLimit
		}
		check.plan, err = session.NewInvalidPreCompactPlan(cause)
		return check, protocol.CompactionFailureHookConfigInvalid, err
	}
	if len(declarations) == 0 {
		check.plan = session.NewNoPreCompactPlan()
		return check, "", nil
	}
	check.declarations = declarations
	check.plan, err = session.NewPreCompactPlan(id, scope, preview.Source(), preview.Input().Instructions(), declarations)
	return check, "", err
}

type preCompactOutcome struct {
	preview  provider.CompactionPreview
	finished int
	code     protocol.CompactionFailureCode
	cause    domain.HookCode
}

func (outcome preCompactOutcome) fail(code protocol.CompactionFailureCode, cause domain.HookCode) preCompactOutcome {
	outcome.code, outcome.cause = code, cause
	return outcome
}

func (runtime *Runtime) runPreCompact(ctx context.Context, id domain.CompactionID, scope domain.CompactionScope, admission compactionAdmission, check preCompactCheck, emit Emitter) preCompactOutcome {
	outcome := runtime.checkPreCompact(ctx, id, scope, admission, check, emit)
	if runtime.poisoned.Load() {
		return outcome
	}
	draft, err := session.NewPreCompactHookDecidedDraft(id, scope, check.plan, outcome.finished, outcome.code, outcome.cause)
	if err == nil {
		_, err = runtime.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{draft})
	}
	if err != nil {
		runtime.poisoned.Store(true)
		return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
	}
	return outcome
}

func (runtime *Runtime) checkPreCompact(ctx context.Context, id domain.CompactionID, scope domain.CompactionScope, admission compactionAdmission, check preCompactCheck, emit Emitter) preCompactOutcome {
	outcome := preCompactOutcome{preview: admission.preview, cause: domain.HookOK}
	if ctx.Err() != nil {
		return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
	}
	if !runtime.interactiveApprovals {
		return outcome.fail(protocol.CompactionFailureHookTrustRequired, domain.HookTrustRequired)
	}
	if check.availability != domain.HookOK {
		return outcome.fail(protocol.CompactionFailureHookFailed, check.availability)
	}
	if code := check.runner.Preflight(ctx); code != domain.HookOK {
		if ctx.Err() != nil {
			return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
		}
		return outcome.fail(protocol.CompactionFailureHookFailed, code)
	}
	original := admission.preview.Input().Instructions()
	input, err := domain.NewPreCompactHookInput(runtime.sessionID, runtime.threadID, id, scope, check.workspace, original)
	if err != nil {
		return outcome.fail(protocol.CompactionFailureHookFailed, domain.HookInputLimit)
	}
	target, err := domain.NewPreCompactHookTarget(id, scope)
	if err != nil {
		runtime.poisoned.Store(true)
		return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
	}
	for _, declaration := range check.declarations {
		if ctx.Err() != nil {
			return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
		}
		code, trustErr := runtime.trustHook(ctx, emit, scope.TurnID, target, check.plan.Digest(), declaration)
		if ctx.Err() != nil {
			return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
		}
		if trustErr != nil {
			runtime.poisoned.Store(true)
			return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
		}
		if code != domain.HookOK {
			return outcome.fail(protocol.CompactionFailureHookTrustDenied, domain.HookTrustDenied)
		}
	}
	segments := make([]string, 0, len(check.declarations)+1)
	if original != "" {
		segments = append(segments, original)
	}
	baseCount := len(segments)
	remaining := 30 * time.Second
	for ordinal, declaration := range check.declarations {
		if ctx.Err() != nil {
			return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
		}
		if remaining <= 0 {
			return outcome.fail(protocol.CompactionFailureHookFailed, domain.HookExecutionBudgetExhausted)
		}
		identity, err := domain.NewPreCompactHookRunIdentity(target, check.plan.Digest(), ordinal)
		if err != nil {
			runtime.poisoned.Store(true)
			return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
		}
		started, err := session.NewPreCompactHookStartedDraft(identity)
		if err == nil {
			_, err = runtime.commitHookFacts(ctx, []session.RecordDraft{started})
		}
		if err != nil {
			if ctx.Err() != nil && !runtime.poisoned.Load() {
				return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
			}
			runtime.poisoned.Store(true)
			return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
		}
		activity := protocol.HookActivityPayload{Target: target, PlanDigest: check.plan.Digest(), Reference: declaration.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK}
		runtime.publishPreCompactActivity(emit, scope.TurnID, activity)
		execution, left := runTimedHook(ctx, check.runner, check.now, remaining, identity, declaration, input)
		remaining = left
		result := execution.Result
		focus := execution.Feedback.Content()
		if !validHookExecution(execution, identity, declaration.Reference.Feedback) {
			result = domain.HookRunResult{Identity: identity, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
			focus = ""
		}
		finished, err := session.NewPreCompactHookFinishedDraft(result, focus)
		if err == nil {
			_, err = runtime.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{finished})
		}
		if err != nil {
			runtime.poisoned.Store(true)
			return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
		}
		outcome.finished++
		activity.State, activity.Code = protocol.HookActivityState(result.Status), result.Code
		activity.StdoutBytes, activity.StderrBytes = result.StdoutBytes, result.StderrBytes
		runtime.publishPreCompactActivity(emit, scope.TurnID, activity)
		switch result.Status {
		case domain.HookContinue:
			if focus != "" {
				segments = append(segments, focus)
			}
		case domain.HookUncertain:
			return outcome.fail(protocol.CompactionFailureSession, result.Code)
		case domain.HookBlock:
			return outcome.fail(protocol.CompactionFailureHookDenied, result.Code)
		case domain.HookCancel:
			return outcome.fail(protocol.CompactionFailureCancelled, result.Code)
		default:
			return outcome.fail(protocol.CompactionFailureHookFailed, result.Code)
		}
	}
	if ctx.Err() != nil {
		return outcome.fail(protocol.CompactionFailureCancelled, domain.HookCancelled)
	}
	if len(segments) == baseCount {
		return outcome
	}
	return runtime.recheckCompactionFocus(admission, outcome, strings.Join(segments, "\n\n"))
}

func (runtime *Runtime) recheckCompactionFocus(admission compactionAdmission, outcome preCompactOutcome, focus string) preCompactOutcome {
	input, err := admission.preview.Input().WithInstructions(focus)
	if err != nil {
		return outcome.fail(protocol.CompactionFailureBudget, domain.HookFeedbackBudgetExceeded)
	}
	preview, _, err := admission.kernel.PreflightCompaction(input)
	if err != nil {
		var failure *provider.CompactionError
		if codec.IsCanonicalSizeLimit(err) || errors.As(err, &failure) && failure.Code() == provider.CompactionBudgetExceeded {
			return outcome.fail(protocol.CompactionFailureBudget, domain.HookFeedbackBudgetExceeded)
		}
	}
	if err != nil || preview.Source() != admission.preview.Source() {
		runtime.poisoned.Store(true)
		return outcome.fail(protocol.CompactionFailureSession, domain.HookOutcomeUncertain)
	}
	if err := contextplan.CheckCompactionRequest(preview.Estimate(), input.OutputTokens(), runtime.budget); err != nil {
		return outcome.fail(compactionFailureCode(context.Background(), err), domain.HookFeedbackBudgetExceeded)
	}
	outcome.preview = preview
	return outcome
}

func (runtime *Runtime) publishPreCompactActivity(emit Emitter, turn domain.TurnID, activity protocol.HookActivityPayload) {
	event, err := protocol.NewHookActivity(activity)
	if err == nil {
		emit(runtime.decorate(event, turn))
	}
}
