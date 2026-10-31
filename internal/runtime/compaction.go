package runtime

import (
	"context"
	"errors"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// contextPlan 读取已提交有效窗口，不包含待处理输入和流式 staging。
func (runtime *Runtime) contextPlan() (contextplan.ContextPlan, error) {
	view, err := runtime.conversation.ProjectActiveContext()
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	footprint, err := runtime.conversation.HistoryFootprint()
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	return runtime.planWindow(view, footprint)
}
func (runtime *Runtime) planWindow(view domain.ActiveContextView, footprint domain.NativeHistoryFootprint) (contextplan.ContextPlan, error) {
	input, err := contextplan.NewPlanningInput(runtime.profile, runtime.toolCatalog, runtime.projectInstructions, view, footprint, contextplan.NewSamplingInput(), runtime.budget)
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	return runtime.planner.Plan(input)
}
func (runtime *Runtime) contextSnapshot() (protocol.ContextSnapshotPayload, error) {
	plan, err := runtime.contextPlan()
	if err != nil {
		return protocol.ContextSnapshotPayload{}, err
	}
	view, err := runtime.conversation.ProjectActiveContext()
	if err != nil {
		return protocol.ContextSnapshotPayload{}, err
	}
	total, err := protocol.NewContextEstimatePayload(plan.TotalEstimate())
	if err != nil {
		return protocol.ContextSnapshotPayload{}, err
	}
	snapshot := protocol.ContextSnapshotPayload{Scope: "committed", Revision: view.Revision(), Activity: "idle", Model: runtime.profile.Model(), Sources: make([]protocol.ContextSourcePayload, 0), Total: total, WindowConfigured: runtime.budget.Enabled(), Window: runtime.budget.Window(), ReservedOutput: runtime.budget.ReservedOutput(), SafetyMargin: runtime.budget.SafetyMargin(), BudgetState: string(plan.Decision().State())}
	for _, source := range plan.Sources() {
		estimate, err := protocol.NewContextEstimatePayload(source.Estimate())
		if err != nil {
			return protocol.ContextSnapshotPayload{}, err
		}
		snapshot.Sources = append(snapshot.Sources, protocol.ContextSourcePayload{Kind: string(source.Kind()), Revision: source.Revision(), Estimate: estimate})
	}
	return snapshot, snapshot.Validate()
}

type compactionAdmission struct {
	kernel  provider.CompactionKernel
	preview provider.CompactionPreview
	before  contextplan.ContextPlan
}

func (runtime *Runtime) preflightCompaction(instructions string) (compactionAdmission, protocol.ControlErrorCode) {
	kernel, ok := runtime.conversation.(provider.CompactionKernel)
	if !ok {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	sampling, err := provider.NewSamplingInput().WithProjectInstructions(runtime.projectInstructions)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	sampling, err = sampling.WithToolCatalog(runtime.toolCatalog)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	input, err := provider.NewCompactionInput(sampling, instructions, runtime.compactionOutputTokens)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	preview, err := kernel.PreflightCompaction(input)
	if err != nil {
		var failure *provider.CompactionError
		if errors.As(err, &failure) {
			switch failure.Code() {
			case provider.CompactionNothing:
				return compactionAdmission{}, protocol.ControlErrorNothingToCompact
			case provider.CompactionBusy:
				return compactionAdmission{}, protocol.ControlErrorSessionBusy
			}
		}
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	if err := contextplan.CheckCompactionRequest(preview.Estimate(), input.OutputTokens(), runtime.budget); err != nil {
		var rejection *contextplan.CompactionBudgetError
		if errors.As(err, &rejection) && rejection.Reason() == contextplan.CompactionBudgetUnknown {
			return compactionAdmission{}, protocol.ControlErrorCompactionBudgetUnknown
		}
		return compactionAdmission{}, protocol.ControlErrorCompactionBudgetExceeded
	}
	before, err := runtime.contextPlan()
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	return compactionAdmission{kernel: kernel, preview: preview, before: before}, ""
}

func (runtime *Runtime) runCompaction(ctx context.Context, id domain.CompactionID, admission compactionAdmission, emit Emitter) error {
	if !runtime.active.CompareAndSwap(false, true) {
		return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
	}
	defer runtime.active.Store(false)
	started, err := session.NewCompactionStartedDraft(id, admission.preview.Source())
	if err != nil {
		return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
	}
	if _, err = runtime.journal.AppendBatch(ctx, []session.RecordDraft{started}); err != nil {
		if runtime.journal.Poisoned() {
			runtime.poisoned.Store(true)
		}
		return runtime.failCompaction(id, compactionFailureCode(ctx, err), false, nil, emit)
	}
	event, err := protocol.NewCompactionStarted(runtime.sessionID, runtime.threadID, id, admission.preview.Source().ContextRevision())
	if err != nil {
		runtime.poisoned.Store(true)
		return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
	}
	emit(event)
	networkCtx, cancel := runtime.compactionDeadline(ctx, 120*time.Second)
	prepared, err := admission.kernel.RunCompaction(networkCtx, id, admission.preview)
	code := compactionFailureCode(networkCtx, err)
	cancel()
	if err != nil {
		return runtime.failCompaction(id, code, true, nil, emit)
	}
	view, footprint, err := prepared.Preview()
	if err != nil {
		return runtime.failCompaction(id, protocol.CompactionFailureResponse, true, prepared, emit)
	}
	candidate, err := runtime.planWindow(view, footprint)
	if err == nil {
		err = contextplan.CheckCompactionCandidate(admission.before, candidate)
	}
	if err != nil {
		return runtime.failCompaction(id, compactionFailureCode(ctx, err), true, prepared, emit)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil || checkpoint.ID() != id || checkpoint.Source() != admission.preview.Source() {
		return runtime.failCompaction(id, protocol.CompactionFailureResponse, true, prepared, emit)
	}
	usage, err := prepared.Usage()
	if err != nil {
		return runtime.failCompaction(id, protocol.CompactionFailureResponse, true, prepared, emit)
	}
	completed, err := session.NewCompactionCompletedDraft(checkpoint, usage, admission.before.TotalEstimate(), candidate.TotalEstimate())
	if err != nil {
		return runtime.failCompaction(id, protocol.CompactionFailureResponse, true, prepared, emit)
	}
	records, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{completed})
	if err != nil {
		if runtime.journal.Poisoned() {
			runtime.poisoned.Store(true)
			return runtime.failCompaction(id, protocol.CompactionFailureSession, false, prepared, emit)
		}
		return runtime.failCompaction(id, compactionFailureCode(ctx, err), true, prepared, emit)
	}
	// writer admission 后的确定提交结果优先于取消，窗口必须安装且只发送 completed。
	if err = prepared.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
	}
	if runtime.compactionHistory != nil {
		summary, _ := view.Summary()
		if len(records) != 1 || runtime.compactionHistory.CompactionCommitted(records[0].Sequence, id, summary) != nil {
			runtime.poisoned.Store(true)
			return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
		}
	}
	event, err = protocol.NewCompactionCompleted(runtime.sessionID, runtime.threadID, id, view.Revision(), admission.before.TotalEstimate(), candidate.TotalEstimate(), usage)
	if err != nil {
		runtime.poisoned.Store(true)
		return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
	}
	emit(event)
	return nil
}

func (runtime *Runtime) failCompaction(id domain.CompactionID, code protocol.CompactionFailureCode, started bool, prepared *provider.PreparedCompaction, emit Emitter) error {
	payload := session.CompactionFailedPayload{ID: id, Code: code, UsageState: "unavailable"}
	eventPayload := protocol.CompactionFailedPayload{Code: code, UsageState: "unavailable"}
	if prepared != nil {
		usage, usageErr := prepared.Usage()
		provenance, provenanceErr := prepared.UsageProvenance()
		if usageErr == nil && provenanceErr == nil {
			value, err := session.NewSampleUsagePayload(usage)
			if err == nil {
				payload.UsageState = "observed"
				payload.Usage = &value
				payload.Provenance = &session.NativeCommitPayload{Provider: provenance.Family(), Wire: provenance.Wire(), PayloadVersion: provenance.PayloadVersion(), Payload: provenance.Payload()}
				// 公共事件只接收归一化计量，绝不复制 Provider raw usage。
				usagePayload, err := protocol.NewUsagePayload(usage)
				if err == nil {
					eventPayload.UsageState = "observed"
					eventPayload.Usage = &usagePayload
				}

			}
		}
		if err := prepared.Discard(); err != nil {
			runtime.poisoned.Store(true)
		}
	}
	if started && !runtime.poisoned.Load() && !runtime.journal.Poisoned() {
		draft, err := session.NewCompactionFailedDraft(payload)
		if err == nil {
			_, err = runtime.journal.AppendBatch(context.Background(), []session.RecordDraft{draft})
		}
		if err == nil {
			eventPayload.Durable = true
		} else {
			runtime.poisoned.Store(true)
		}
	}
	event, err := protocol.NewCompactionFailed(runtime.sessionID, runtime.threadID, id, eventPayload)
	if err == nil {
		emit(event)
	} else {
		runtime.poisoned.Store(true)
	}
	return fault.New(fault.CodeTurnFailed, string(code))
}

func compactionFailureCode(ctx context.Context, err error) protocol.CompactionFailureCode {
	if ctx.Err() == context.DeadlineExceeded {
		return protocol.CompactionFailureTimeout
	}
	if ctx.Err() != nil {
		return protocol.CompactionFailureCancelled
	}
	var budget *contextplan.CompactionBudgetError
	if errors.As(err, &budget) {
		switch budget.Reason() {
		case contextplan.CompactionNoReduction:
			return protocol.CompactionFailureNoReduction
		case contextplan.CompactionBudgetUnknown:
			return protocol.CompactionFailureUnknownBudget
		default:
			return protocol.CompactionFailureBudget
		}
	}
	var failure *provider.CompactionError
	if errors.As(err, &failure) {
		switch failure.Code() {
		case provider.CompactionCancelled:
			return protocol.CompactionFailureCancelled
		case provider.CompactionTimedOut:
			return protocol.CompactionFailureTimeout
		case provider.CompactionInvalidResponse, provider.CompactionSourceChanged:
			return protocol.CompactionFailureResponse
		}
	}
	return protocol.CompactionFailureRequest
}
