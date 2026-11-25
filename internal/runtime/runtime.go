// Package runtime 实现共享 turn 模板和 durable 生命周期编排。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

const (
	maxSamplesPerTurn      = 16
	maxToolCallsPerTurn    = 64
	maxParallelReadWorkers = 8
)

// Emitter 接收 Runtime 产生的共享语义事件。
type Emitter func(protocol.Event)

// Journal 是 Runtime 所需的最小 durable Session 能力。
type Journal interface {
	AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error)
	Poisoned() bool
}

// TurnIDGenerator 为每个被接受的 turn 分配稳定 UUIDv7 标识。
type TurnIDGenerator func() (domain.TurnID, error)

// InvocationIDGenerator 为durable ready call分配UUIDv7身份。
type InvocationIDGenerator func() (tool.InvocationID, error)
type ApprovalRequestIDGenerator func() (tool.ApprovalRequestID, error)

// ContextPlanner 是 Runtime 在 Provider 副作用前使用的纯内存规划能力。
type ContextPlanner interface {
	Plan(contextplan.PlanningInput) (contextplan.ContextPlan, error)
}

// Config 固定一个 Session-bound Runtime 的身份与 durable 边界。
type Config struct {
	InitialSessionTitle       domain.SessionTitle
	InitialTaskProgress       domain.TaskProgressState
	InteractiveQuestions      bool
	GenerateQuestionRequestID func() (domain.QuestionRequestID, error)
	AutoCompactionPolicy      contextplan.AutoCompactionPolicy
	CompactionHistory         CompactionHistoryObserver
	CompactionOutputTokens    uint64
	TextOutputStore           tool.TextOutputStore
	ToolHistory               ToolHistoryObserver
	HookHistory               HookHistoryObserver
	StopHistory               StopHistoryObserver
	SessionStartHistory       SessionStartHistoryObserver
	ArtifactReferences        *tool.ArtifactReferences
	SessionID                 domain.SessionID
	ThreadID                  domain.ThreadID
	Journal                   Journal
	GenerateTurnID            TurnIDGenerator
	GenerateInvocationID      InvocationIDGenerator
	InterruptedTail           bool
	CachePolicy               domain.CachePolicy
	RestoredCacheBaseline     bool
	ContextProfile            contextplan.ProviderProfile
	ProviderWire              string
	ToolBinding               tool.Binding
	GenerateApprovalRequestID ApprovalRequestIDGenerator
	InteractiveApprovals      bool
	ProjectInstructions       domain.ProjectInstructionsSnapshot
	ContextBudget             contextplan.Budget
	ContextPlanner            ContextPlanner
}

// Runtime 持有一条会话级 Provider Conversation 和同一 thread journal。
type Runtime struct {
	initialSessionTitle  domain.SessionTitle
	tasks                taskProgressProjection
	interactiveQuestions bool
	newQuestionRequestID func() (domain.QuestionRequestID, error)
	questions            questionState

	hooks                        hookState
	skillInstallation            skillInstallation
	inputPreparer                InputPreparer
	userInputCommitted           func(domain.UserInput) (protocol.Event, error)
	skillCatalog                 domain.SkillCatalog
	explicitSkills               []domain.SkillAttachment
	snapshotPublisher            func(runtimePublication)
	cachePolicy                  domain.CachePolicy
	restoredCacheBaseline        bool
	cache                        cacheDiagnostics
	autoCompactionFailures       uint8
	autoCompactionFailedRevision uint64
	autoCompactionPolicy         contextplan.AutoCompactionPolicy
	compactionHistory            CompactionHistoryObserver
	compactionOutputTokens       uint64
	compactionDeadline           func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	turnEmitter                  Emitter
	toolTurn                     toolTurnState
	textOutputStore              tool.TextOutputStore
	toolHistory                  ToolHistoryObserver
	hookHistory                  HookHistoryObserver
	stopHistory                  StopHistoryObserver
	sessionStartHistory          SessionStartHistoryObserver
	sessionStartSource           domain.SessionStartSource
	artifactReferences           *tool.ArtifactReferences
	conversation                 provider.Conversation
	providerOperations           providerOperations
	providerWire                 string
	journal                      Journal
	sessionID                    domain.SessionID
	threadID                     domain.ThreadID
	newTurnID                    TurnIDGenerator
	newInvocationID              InvocationIDGenerator
	profile                      contextplan.ProviderProfile
	operationProfile             contextplan.ProviderProfile
	tools                        tool.Components
	toolCatalog                  tool.CatalogSnapshot
	planToolCatalog              tool.CatalogSnapshot
	newApprovalRequestID         ApprovalRequestIDGenerator
	interactiveApprovals         bool
	readObservations             readObservations
	approvals                    approvalState
	projectInstructions          domain.ProjectInstructionsSnapshot
	budget                       contextplan.Budget
	planner                      ContextPlanner
	active                       atomic.Bool
	poisoned                     atomic.Bool
	samplePresentation           *samplePresentationState
}

// New 创建已绑定稳定 Session 身份的共享 Runtime。
func New(conversation provider.Conversation, config Config) (*Runtime, error) {
	if conversation == nil {
		return nil, fault.New(fault.CodeProviderUnavailable, "provider conversation is not configured")
	}
	if err := domain.WithDefaultCachePolicy(config.CachePolicy).ValidateFamily(conversation.Family()); err != nil {
		return nil, err
	}
	if !config.SessionID.Valid() || !config.ThreadID.Valid() {
		return nil, fault.New(fault.CodeTurnFailed, "runtime session identity is invalid")
	}
	if config.InitialSessionTitle != "" && config.InitialSessionTitle.Validate() != nil {
		return nil, fault.New(fault.CodeSessionCorruption, "session title is invalid")
	}
	if config.Journal == nil {
		return nil, fault.New(fault.CodeSessionWrite, "session journal is not configured")
	}
	if config.InterruptedTail {
		return nil, fault.New(fault.CodeSessionCorruption, "interrupted session turn must be closed before runtime starts")
	}
	if err := config.ContextProfile.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime context profile is invalid", err)
	}
	if config.ContextProfile.WorkMode() != domain.WorkModeDefault {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime work mode must start in default")
	}
	if config.ContextProfile.Family() != conversation.Family() {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime context profile provider does not match conversation")
	}
	if config.ProviderWire != conversation.Family().Wire() {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime provider wire does not match conversation")
	}
	catalog := config.ToolBinding.Catalog()
	if err := catalog.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime tool catalog is invalid", err)
	}
	if _, err := catalog.View(conversation.Family()); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime tool catalog Provider view is invalid", err)
	}
	planCatalog, err := catalog.ForPlanMode()
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime plan tool catalog is invalid", err)
	}
	if config.GenerateQuestionRequestID == nil {
		config.GenerateQuestionRequestID = domain.GenerateQuestionRequestID
	}
	if err := config.InitialTaskProgress.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime task progress is invalid", err)
	}
	if config.GenerateApprovalRequestID == nil {
		config.GenerateApprovalRequestID = tool.GenerateApprovalRequestID
	}
	projectInstructions, err := config.ProjectInstructions.Clone()
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime project instructions are invalid", err)
	}
	if err := config.ContextBudget.ValidateInference(config.ContextProfile.InferenceSettings()); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime context budget is invalid", err)
	}
	if config.ContextPlanner == nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime context planner is not configured")
	}
	if config.CompactionOutputTokens == 0 {
		config.CompactionOutputTokens = 4096
	}
	if config.CompactionOutputTokens > 65536 {
		return nil, fault.New(fault.CodeInvalidConfiguration, "compaction output tokens are invalid")
	}
	if config.GenerateTurnID == nil {
		config.GenerateTurnID = domain.GenerateTurnID
	}
	if config.GenerateInvocationID == nil {
		config.GenerateInvocationID = tool.GenerateInvocationID
	}
	if config.ArtifactReferences == nil {
		var err error
		config.ArtifactReferences, err = tool.NewArtifactReferences(config.ThreadID, nil)
		if err != nil {
			return nil, fault.New(fault.CodeInvalidConfiguration, "artifact references are invalid")
		}
	}
	return &Runtime{
		initialSessionTitle:    config.InitialSessionTitle,
		tasks:                  taskProgressProjection{state: config.InitialTaskProgress.Clone()},
		autoCompactionPolicy:   config.AutoCompactionPolicy,
		compactionHistory:      config.CompactionHistory,
		compactionDeadline:     context.WithTimeout,
		compactionOutputTokens: config.CompactionOutputTokens,
		toolHistory:            config.ToolHistory,
		hookHistory:            config.HookHistory,
		stopHistory:            config.StopHistory,
		sessionStartHistory:    config.SessionStartHistory,
		interactiveQuestions:   config.InteractiveQuestions, newQuestionRequestID: config.GenerateQuestionRequestID,
		userInputCommitted: protocol.NewUserInputCommitted,
		textOutputStore:    config.TextOutputStore,
		artifactReferences: config.ArtifactReferences,
		conversation:       conversation, providerOperations: bindProviderOperations(conversation), providerWire: config.ProviderWire, journal: config.Journal,
		sessionID: config.SessionID, threadID: config.ThreadID, newTurnID: config.GenerateTurnID,
		newInvocationID: config.GenerateInvocationID,
		cachePolicy:     domain.WithDefaultCachePolicy(config.CachePolicy), restoredCacheBaseline: config.RestoredCacheBaseline,
		profile: config.ContextProfile, operationProfile: config.ContextProfile,
		toolCatalog: catalog, planToolCatalog: planCatalog, tools: config.ToolBinding.Components(),
		newApprovalRequestID: config.GenerateApprovalRequestID, interactiveApprovals: config.InteractiveApprovals, projectInstructions: projectInstructions,
		budget: config.ContextBudget, planner: config.ContextPlanner,
	}, nil
}

// runTurn 执行一次文本 turn，并等待 Provider terminal 后的 durable 收口。
func (runtime *Runtime) runTurn(ctx context.Context, inputs []domain.UserInput, profile contextplan.ProviderProfile, control turnInputControl, emit Emitter) (resultErr error) {
	if err := runtime.validateTurnRequest(ctx, emit); err != nil {
		return err
	}
	runtime.markSkillsStarted()
	if !runtime.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	defer runtime.active.Store(false)
	runtime.operationProfile = profile
	defer func() { runtime.operationProfile = runtime.profile }()
	runtime.samplePresentation = nil
	defer func() { runtime.samplePresentation = nil }()
	runtime.resetToolTurn()
	runtime.explicitSkills = nil
	defer func() { runtime.explicitSkills = nil }()
	defer func() { runtime.releaseFailedContinuation(ctx, resultErr) }()
	runtime.turnEmitter = emit
	defer func() { runtime.turnEmitter = nil }()
	defer runtime.clearTurnObservations()
	runtime.toolTurn.resultBudget = tool.NewResultBudget()
	defer func() {
		runtime.toolTurn.resultBudget = nil
		runtime.toolTurn.resetResultTicket()
	}()

	turnID, err := runtime.beginTurn(ctx, emit)
	if err != nil {
		return err
	}
	promptBudget := 30 * time.Second
	inputs, rejected, err := runtime.checkPromptSubmissions(ctx, emit, turnID, 0, inputs, control, &promptBudget)
	if err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
	if rejected != "" && len(inputs) == 0 {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, promptSubmitFailure(rejected))
	}
	if err := runtime.prepareAutomaticSample(ctx, turnID, 0, inputs, emit); err != nil {
		if control != nil {
			reason := protocol.InputReasonFailed
			if ctx.Err() != nil {
				reason = protocol.InputReasonCancelled
			}
			control.resolve(turnID, inputs, protocol.InputReturned, reason)
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
	result := runtime.commitInitialInputs(ctx, turnID, inputs, control, emit)
	if result.err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, result.err)
	}
	usages := make([]domain.SampleUsage, 0, maxSamplesPerTurn)
	totalCalls := 0
	stopBudget := stopTurnBudget{remaining: 30 * time.Second}
	seenInvocationIDs := make(map[tool.InvocationID]struct{})
	for sampleIndex := 0; ; sampleIndex++ {
		if sampleIndex >= maxSamplesPerTurn {
			failure := fault.New(fault.CodeTurnFailed, "tool_loop_limit_exceeded")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
		}
		runtime.startSamplePresentation(uint32(sampleIndex))
		terminal, sampleErr := runtime.runProviderSample(ctx, emit, turnID)
		if sampleErr != nil {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, sampleErr)
		}
		if terminal.Kind() != provider.StreamEventCompleted {
			return runtime.finishFailedProviderStream(ctx, emit, turnID, terminal)
		}
		sample := terminal.PreparedSample()
		ready, usage, inspectErr := inspectPreparedSample(sample)
		if inspectErr == nil {
			inspectErr = runtime.prepareSamplePresentation(sample)
		}
		if inspectErr != nil {
			discardPreparedSample(sample)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, inspectErr)
		}
		if len(ready) == 0 {
			usages = append(usages, usage)
			check, err := runtime.completionCheck(domain.AssistantSampleReference{TurnID: turnID, SampleIndex: uint32(sampleIndex)}, stopBudget)
			if err != nil {
				discardPreparedSample(sample)
				return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
			}
			if check == nil {
				return runtime.commitFinalSample(ctx, emit, turnID, sample, usages)
			}
			if err := runtime.commitStopSample(ctx, turnID, sample, check); err != nil {
				runtime.emitFailure(emit, turnID, err)
				return err
			}
			continued, err := runtime.runStopCheck(ctx, emit, check, &stopBudget, usages)
			if !continued {
				if err != nil && check.state.Decision == nil {
					runtime.emitFailure(emit, turnID, err)
				}
				return err
			}
			if err := runtime.deliverStopContinuation(ctx, emit, check, control, &promptBudget); err != nil {
				return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
			}
			continue
		}
		if totalCalls+len(ready) > maxToolCallsPerTurn {
			discardPreparedSample(sample)
			failure := fault.New(fault.CodeTurnFailed, "tool_loop_limit_exceeded")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
		}
		invocations, commitErr := runtime.commitCallSample(
			ctx, turnID, uint32(sampleIndex), sample, ready, usage, seenInvocationIDs,
		)
		if commitErr != nil {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, commitErr)
		}
		usages = append(usages, usage)
		totalCalls += len(invocations)
		results, executeErr := runtime.executeToolCalls(ctx, turnID, invocations, emit)
		if len(results) != len(invocations) {
			// 保留待恢复的工具组尾部，禁止在尚未配对时写入 turn_failed。
			runtime.poisoned.Store(true)
			if executeErr == nil {
				executeErr = fault.New(fault.CodeStreamProtocol, "tool results do not match the committed call group")
			}
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, executeErr)
		}
		if outputErr := runtime.commitToolOutputs(ctx, turnID, results); outputErr != nil {
			runtime.poisoned.Store(true)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, outputErr)
		}
		if runtime.toolTurn.resultBudget.BodyExhausted() {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, fault.New(fault.CodeToolResultBudgetExceeded, "tool result budget exhausted"))
		}
		if executeErr != nil {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, executeErr)
		}
		if ctx.Err() != nil {
			cancelled := fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, cancelled)
		}
		var pending []domain.UserInput
		if control != nil && sampleIndex+1 < maxSamplesPerTurn && totalCalls < maxToolCallsPerTurn {
			claimed, err := control.claim(ctx, turnID, uint32(sampleIndex+1))
			if err != nil {
				return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
			}
			pending = claimed
		}
		if sampleIndex+1 < maxSamplesPerTurn && totalCalls < maxToolCallsPerTurn {
			pending, _, err = runtime.checkPromptSubmissions(ctx, emit, turnID, uint32(sampleIndex+1), pending, control, &promptBudget)
			if err != nil {
				return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
			}
			if err := runtime.prepareAutomaticSample(ctx, turnID, uint32(sampleIndex+1), pending, emit); err != nil {
				if control != nil && len(pending) > 0 {
					reason := protocol.InputReasonFailed
					if ctx.Err() != nil {
						reason = protocol.InputReasonCancelled
					}
					control.resolve(turnID, pending, protocol.InputReturned, reason)
				}
				return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
			}
			if len(pending) > 0 {
				result := runtime.commitInputs(ctx, turnID, uint32(sampleIndex+1), pending, control, emit)
				if result.err != nil && !result.preflightRejected {
					return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, result.err)
				}
			}
		}
	}
}

// releaseFailedContinuation 在失败或取消的操作收口时，由原 worker 清除瞬态续写资格。
func (runtime *Runtime) releaseFailedContinuation(ctx context.Context, operationErr error) {
	if operationErr == nil && ctx.Err() == nil {
		return
	}
	if resetter := runtime.providerOperations.continuation; resetter != nil {
		// 已有操作错误；若会话有其他 owner，由它负责在当前生命周期收口。
		_ = resetter.ResetContinuation()
	}
}

// ReconcileToolTurn 在宿主接收新输入前，以本地durable事实关闭未完成工具turn。
func (runtime *Runtime) ReconcileToolTurn(ctx context.Context, plan session.ToolRecoveryPlan) error {
	if ctx == nil {
		return fault.New(fault.CodeSessionCorruption, "tool recovery context is required")
	}
	if runtime == nil || runtime.conversation == nil || runtime.journal == nil {
		return fault.New(fault.CodeSessionCorruption, "tool recovery runtime is not configured")
	}
	if err := plan.Validate(); err != nil {
		return fault.Wrap(fault.CodeSessionCorruption, "tool recovery plan is invalid", err)
	}
	if runtime.poisoned.Load() || runtime.journal.Poisoned() {
		return fault.New(fault.CodeSessionWrite, "session journal is poisoned")
	}
	if !runtime.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	defer runtime.active.Store(false)
	defer runtime.clearTurnObservations()

	reconcileContext := context.WithoutCancel(ctx)
	runtime.clearFileState()
	calls := plan.Calls()
	for _, call := range calls {
		if err := runtime.registerToolCall(call.Origin, call.Invocation); err != nil {
			return err
		}
		state := runtime.toolTurn.toolCalls[call.Invocation.InvocationID()]
		state.resultOrigin = tool.InputResultRecovery
		if call.Hooks != nil {
			g := call.Hooks.Clone()
			state.pre = &g
		}
		admission, err := call.ExecutionAdmission()
		if err != nil {
			return err
		}
		state.admission = admission
		if call.State == session.ReplayedToolCallResult {
			if call.Result.Budget().Reason() != tool.BudgetRecovery {
				state.resultOrigin = tool.InputResultExecution
			}
			if err := runtime.rememberToolResult(plan.TurnID(), call.Result); err != nil {
				return err
			}
		}
	}
	results := make([]tool.InvocationResult, 0, len(calls))
	outcomeUncertain := false
	for _, call := range calls {
		recoveryAllowance := tool.SafetyResultBytes
		if call.PostHooks != nil {
			recoveryAllowance = call.PostHooks.Plan.Allowance - call.PostHooks.Plan.Reservation
		}
		if call.Hooks != nil && call.Hooks.Decision == nil {
			result, err := runtime.reconcileHookGate(reconcileContext, plan.TurnID(), call)
			if err != nil {
				return err
			}
			results = append(results, result)
			outcomeUncertain = outcomeUncertain || call.Hooks.Started != nil
			continue
		}
		effective, effectiveErr := call.ResultInvocation()
		if effectiveErr != nil {
			return effectiveErr
		}
		var result tool.InvocationResult
		switch call.State {
		case session.ReplayedToolCallReady:
			code := "session_interrupted_before_execution"
			preview := "Tool cancelled: session interrupted before execution"
			if call.Invocation.Capability().RequiresApproval() && call.ApprovalSequence == 0 {
				code = "session_interrupted_before_approval"
				preview = "Tool cancelled: session interrupted before approval"
				if call.Invocation.Capability() == tool.CapabilityExec {
					preview = "Exec cancelled: session interrupted before approval"
				}
			}
			result = tool.NewInvocationErrorResult(
				effective, tool.ResultCancelled, code, preview,
			)
			result, _ = tool.FinalizeResult(result, recoveryAllowance, tool.BudgetRecovery)
			if err := runtime.persistToolResult(reconcileContext, plan.TurnID(), &result); err != nil {
				return err
			}
		case session.ReplayedToolCallStarted:
			result = tool.NewInvocationErrorResult(
				effective, tool.ResultOutcomeUncertain, "outcome_uncertain",
				"Tool failed: execution outcome is uncertain",
			)
			result, _ = tool.FinalizeResult(result, recoveryAllowance, tool.BudgetRecovery)
			if err := runtime.persistToolResult(reconcileContext, plan.TurnID(), &result); err != nil {
				return err
			}
			outcomeUncertain = true
		case session.ReplayedToolCallResult:
			result = call.Result
			outcomeUncertain = outcomeUncertain || result.Status() == tool.ResultOutcomeUncertain || call.Hooks != nil && call.Hooks.Decision != nil && call.Hooks.Decision.Status == domain.HookUncertain
		default:
			return fault.New(fault.CodeSessionCorruption, "tool recovery call state is invalid")
		}
		postUncertain, err := runtime.reconcilePostHooks(reconcileContext, plan.TurnID(), call, result)
		if err != nil {
			return err
		}
		outcomeUncertain = outcomeUncertain || postUncertain
		results = append(results, result)
	}
	if !plan.ToolOutputsCommitted() {
		if err := runtime.commitToolOutputs(reconcileContext, plan.TurnID(), results); err != nil {
			return err
		}
	}
	return runtime.closeReconciledToolTurn(reconcileContext, plan.TurnID(), outcomeUncertain)
}

func (runtime *Runtime) closeReconciledToolTurn(
	ctx context.Context,
	turnID domain.TurnID,
	outcomeUncertain bool,
) error {
	code := "session_interrupted"
	message := "session tool turn was interrupted"
	if outcomeUncertain {
		code = "outcome_uncertain"
		message = "tool execution outcome is uncertain"
	}
	draft, err := session.NewTurnFailedDraft(turnID, session.TurnFailedPayload{
		Code: code, Message: message,
	})
	if err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "build reconciled tool turn failure failed", err)
	}
	if _, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{draft}); err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist reconciled tool turn failure failed", err)
	}
	return nil
}

func (runtime *Runtime) validateTurnRequest(ctx context.Context, emit Emitter) error {
	if ctx == nil {
		return fault.New(fault.CodeTurnFailed, "turn context is required")
	}
	if emit == nil {
		return fault.New(fault.CodeTurnFailed, "event emitter is required")
	}
	if runtime == nil || runtime.conversation == nil {
		return fault.New(fault.CodeProviderUnavailable, "provider conversation is not configured")
	}
	if runtime.poisoned.Load() || runtime.journal.Poisoned() {
		return fault.New(fault.CodeSessionWrite, "session journal is poisoned")
	}
	return nil
}

func (runtime *Runtime) beginTurn(ctx context.Context, emit Emitter) (domain.TurnID, error) {
	turnID, err := runtime.newTurnID()
	if err != nil || !turnID.Valid() {
		if err == nil {
			err = fmt.Errorf("generated turn ID is invalid")
		}
		failure := fault.Wrap(fault.CodeTurnFailed, "turn identity generation failed", err)
		runtime.emitFailure(emit, turnID, failure)
		return turnID, failure
	}
	startedDraft, err := session.NewTurnStartedDraft(turnID)
	if err != nil {
		failure := fault.Wrap(fault.CodeTurnFailed, "build turn start failed", err)
		runtime.emitFailure(emit, turnID, failure)
		return turnID, failure
	}
	if _, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{startedDraft}); err != nil {
		failure := fault.Wrap(fault.CodeSessionWrite, "persist turn start failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return turnID, failure
	}
	emit(runtime.decorate(protocol.NewTurnStarted(), turnID))
	return turnID, nil
}

func (runtime *Runtime) planTurnInput() (provider.TurnInput, contextplan.ContextPlan, error) {
	view, footprint, err := runtime.conversation.ProjectContextWindow()
	if err != nil {
		return provider.TurnInput{}, contextplan.ContextPlan{}, err
	}
	return runtime.planHistory(view, footprint)
}

func (runtime *Runtime) consumeProviderStream(
	ctx context.Context,
	cancel context.CancelFunc,
	stream provider.StreamEvents,
	emit Emitter,
	turnID domain.TurnID,
) (provider.StreamEvent, error) {
	var terminal provider.StreamEvent
	terminalSeen := false
	var firstErr error

	for event := range stream {
		if firstErr != nil {
			if event.Kind() == provider.StreamEventCompleted {
				discardPreparedSample(event.PreparedSample())
			}
			continue
		}
		if err := event.Validate(); err != nil {
			firstErr = fault.Wrap(fault.CodeStreamProtocol, "provider emitted an invalid stream event", err)
			cancel()
			continue
		}
		if terminalSeen {
			if event.Kind() == provider.StreamEventCompleted {
				discardPreparedSample(event.PreparedSample())
			}
			firstErr = fault.New(fault.CodeStreamProtocol, "provider emitted an event after terminal")
			cancel()
			continue
		}
		if event.Kind().Terminal() {
			terminal = event
			terminalSeen = true
			continue
		}
		switch event.Kind() {
		case provider.StreamEventSemantic:
			semantic := event.Semantic()
			if semantic.Kind == protocol.EventAssistantReasoning {
				bound, err := runtime.bindReasoning(semantic)
				if err != nil {
					firstErr = fault.Wrap(fault.CodeStreamProtocol, "provider reasoning is invalid", err)
					cancel()
					continue
				}
				payload, _ := protocol.DecodeAssistantReasoning(bound)
				if ctx.Err() == nil || payload.Stage != protocol.ReasoningDelta {
					emit(runtime.decorate(bound, turnID))
				}
			} else if ctx.Err() == nil {
				emit(runtime.decorate(semantic, turnID))
			}
		}
	}

	if firstErr != nil {
		discardPreparedSample(terminal.PreparedSample())
		return provider.StreamEvent{}, firstErr
	}
	if !terminalSeen {
		return provider.StreamEvent{}, fault.New(fault.CodeStreamProtocol, "provider stream closed before terminal")
	}
	return terminal, nil
}

func (runtime *Runtime) runProviderSample(
	ctx context.Context,
	emit Emitter,
	turnID domain.TurnID,
) (provider.StreamEvent, error) {
	providerInput, plan, err := runtime.planTurnInput()
	if err != nil {
		return provider.StreamEvent{}, err
	}
	if err := runtime.freezeCacheSources(plan); err != nil {
		return provider.StreamEvent{}, err
	}
	streamContext, cancelStream := context.WithCancel(ctx)
	stream, err := runtime.conversation.Stream(streamContext, providerInput)
	if err != nil {
		cancelStream()
		return provider.StreamEvent{}, err
	}
	if stream == nil {
		cancelStream()
		return provider.StreamEvent{}, fault.New(fault.CodeStreamProtocol, "provider returned a nil stream")
	}
	terminal, err := runtime.consumeProviderStream(ctx, cancelStream, stream, emit, turnID)
	cancelStream()
	return terminal, err
}

func (runtime *Runtime) finishFailedProviderStream(
	ctx context.Context, emit Emitter, turnID domain.TurnID, terminal provider.StreamEvent,
) error {
	switch terminal.Kind() {
	case provider.StreamEventCancelled:
		err := terminal.Error()
		if err == nil || errors.Is(err, context.Canceled) {
			err = fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	case provider.StreamEventFailed:
		err := terminal.Error()
		if err == nil {
			err = fault.New(fault.CodeProviderRequest, "provider request failed")
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	default:
		failure := fault.New(fault.CodeStreamProtocol, "provider terminal is invalid")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
}

func inspectPreparedSample(sample *provider.PreparedSample) ([]tool.ReadyCall, domain.SampleUsage, error) {
	if _, err := sample.Receipt(); err != nil {
		return nil, domain.SampleUsage{}, fault.Wrap(fault.CodeStreamProtocol, "provider request receipt is invalid", err)
	}
	if sample == nil || sample.Finalized() {
		return nil, domain.SampleUsage{}, fault.New(fault.CodeStreamProtocol, "provider completed without a fresh prepared sample")
	}
	ready, err := sample.ReadyCalls()
	if err != nil {
		return nil, domain.SampleUsage{}, fault.Wrap(fault.CodeStreamProtocol, "provider prepared calls are invalid", err)
	}
	usage, err := sample.Usage()
	if err != nil {
		return nil, domain.SampleUsage{}, fault.Wrap(fault.CodeStreamProtocol, "provider prepared usage is invalid", err)
	}
	return ready, usage, nil
}

func (runtime *Runtime) commitFinalSample(
	ctx context.Context,
	emit Emitter,
	turnID domain.TurnID,
	sample *provider.PreparedSample,
	usages []domain.SampleUsage,
) error {
	defer discardPreparedSample(sample)
	observation, err := runtime.sampleObservation(sample)
	if err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
	envelope, err := sample.Envelope()
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	envelope, err = envelope.Clone()
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	usage, err := sample.Usage()
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "provider prepared usage is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	turnUsage, err := domain.AggregateSampleUsage(usages)
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "turn usage is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	commitDraft, err := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{
		Provider: envelope.Family(), Wire: envelope.Wire(),
		Payload: envelope.Payload(),
	})
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "provider native commit is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	usageDraft, err := session.NewSampleUsageDraft(turnID, usage)
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "sample usage is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	completedDraft, err := session.NewTurnCompletedDraft(turnID)
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "turn completion is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	completedEvent, err := protocol.NewTurnCompleted(turnUsage)
	if err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "turn completion is invalid", err)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	records, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{commitDraft, usageDraft, completedDraft})
	if err != nil {
		runtime.sampleWriteFailed()
		discardPreparedSample(sample)
		failure := fault.Wrap(fault.CodeSessionWrite, "persist completed turn failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	if err := runtime.sampleDurable(records[0].Sequence); err != nil {
		runtime.poisoned.Store(true)
		failure := fault.Wrap(fault.CodeStreamProtocol, "durable sample identity is invalid", err)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	if err := sample.Finalize(); err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "finalize prepared sample failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	runtime.commitSampleObservation(observation)
	turnPayload, _ := protocol.NewUsagePayload(turnUsage)
	runtime.cache.snapshot.LastTurn = &turnPayload
	runtime.publishCacheSnapshot()
	if runtime.toolHistory != nil {
		if err := runtime.toolHistory.NativeCommitted(records[0].Sequence, nil); err != nil {
			runtime.poisoned.Store(true)
			failure := fault.Wrap(fault.CodeStreamProtocol, "transcript projection failed", err)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
	}
	runtime.emitSampleCommit(turnID, records[0].Sequence)
	emit(runtime.decorate(completedEvent, turnID))
	return nil
}

func (runtime *Runtime) commitCallSample(
	ctx context.Context,
	turnID domain.TurnID,
	sampleIndex uint32,
	sample *provider.PreparedSample,
	ready []tool.ReadyCall,
	usage domain.SampleUsage,
	seenInvocationIDs map[tool.InvocationID]struct{},
) ([]tool.Invocation, error) {
	if runtime.operationProfile.WorkMode() == domain.WorkModePlan {
		for _, call := range ready {
			if !tool.PlanModeAllows(call.Capability()) {
				discardPreparedSample(sample)
				return nil, fault.New(fault.CodeStreamProtocol, "tool call is not allowed in plan mode")
			}
		}
	}
	observation, err := runtime.sampleObservation(sample)
	if err != nil {
		discardPreparedSample(sample)
		return nil, err
	}
	envelope, err := sample.Envelope()
	if err != nil {
		discardPreparedSample(sample)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", err)
	}
	commitDraft, err := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{
		Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload(),
	})
	if err != nil {
		discardPreparedSample(sample)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "provider native commit is invalid", err)
	}
	usageDraft, err := session.NewSampleUsageDraft(turnID, usage)
	if err != nil {
		discardPreparedSample(sample)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "sample usage is invalid", err)
	}
	invocations := make([]tool.Invocation, len(ready))
	drafts := make([]session.RecordDraft, 0, len(ready)+2)
	drafts = append(drafts, commitDraft, usageDraft)
	for index, call := range ready {
		if runtime.tools.Policy.Decide(call) == tool.PolicyDeny {
			discardPreparedSample(sample)
			return nil, fault.New(fault.CodeTurnFailed, "tool call is denied")
		}
		invocationID, generateErr := runtime.newInvocationID()
		if generateErr != nil || !invocationID.Valid() {
			discardPreparedSample(sample)
			return nil, fault.New(fault.CodeTurnFailed, "tool invocation identity generation failed")
		}
		if _, duplicate := seenInvocationIDs[invocationID]; duplicate {
			discardPreparedSample(sample)
			return nil, fault.New(fault.CodeTurnFailed, "tool invocation identity is duplicated")
		}
		seenInvocationIDs[invocationID] = struct{}{}
		invocation, invocationErr := tool.NewInvocation(invocationID, call)
		if invocationErr != nil {
			discardPreparedSample(sample)
			return nil, fault.New(fault.CodeStreamProtocol, "provider ready call is invalid")
		}
		readyDraft, draftErr := session.NewToolCallReadyDraft(turnID, invocation, sampleIndex, uint32(index))
		if draftErr != nil {
			discardPreparedSample(sample)
			return nil, fault.Wrap(fault.CodeStreamProtocol, "tool ready fact is invalid", draftErr)
		}
		invocations[index] = invocation
		drafts = append(drafts, readyDraft)
	}
	records, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), drafts)
	if err != nil {
		runtime.sampleWriteFailed()
		discardPreparedSample(sample)
		runtime.poisoned.Store(true)
		return nil, fault.Wrap(fault.CodeSessionWrite, "persist tool call sample failed", err)
	}
	if err := runtime.sampleDurable(records[0].Sequence); err != nil {
		runtime.poisoned.Store(true)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "durable sample identity is invalid", err)
	}
	if err := sample.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "finalize prepared sample failed", err)
	}
	runtime.commitSampleObservation(observation)
	runtime.publishCacheSnapshot()
	for index, invocation := range invocations {
		origin, err := tool.NewToolCallOrigin(runtime.sessionID, runtime.threadID, turnID, sampleIndex, uint32(index))
		if err != nil {
			runtime.poisoned.Store(true)
			return nil, err
		}
		if err := runtime.registerToolCall(origin, invocation); err != nil {
			runtime.poisoned.Store(true)
			return nil, err
		}
	}
	if runtime.toolHistory != nil {
		entries := make([]session.ToolHistoryEntry, len(invocations))
		for index, invocation := range invocations {
			entries[index] = session.ToolHistoryEntry{Origin: runtime.toolTurn.toolCalls[invocation.InvocationID()].origin, Sequence: records[index+2].Sequence, NativeSequence: records[0].Sequence, TurnID: turnID, SampleIndex: sampleIndex, CallIndex: uint32(index), Invocation: invocation}
		}
		if err := runtime.toolHistory.NativeCommitted(records[0].Sequence, entries); err != nil {
			runtime.poisoned.Store(true)
			return nil, err
		}
	}
	runtime.emitSampleCommit(turnID, records[0].Sequence)
	return invocations, nil
}

func (runtime *Runtime) executeToolCalls(ctx context.Context, turnID domain.TurnID, invocations []tool.Invocation, emitters ...Emitter) ([]tool.InvocationResult, error) {
	if runtime.toolTurn.resultBudget == nil {
		runtime.toolTurn.resultBudget = tool.NewResultBudget()
	}
	if err := runtime.toolTurn.resultBudget.BeginSample(len(invocations)); err != nil {
		return nil, err
	}
	runtime.toolTurn.activityEmitter = func(protocol.Event) {}
	if len(emitters) == 1 && emitters[0] != nil {
		runtime.toolTurn.activityEmitter = emitters[0]
	}
	runtime.toolTurn.activityInvocations = make(map[tool.InvocationID]tool.Invocation, len(invocations))
	defer func() { runtime.toolTurn.activityEmitter = nil; runtime.toolTurn.activityInvocations = nil }()
	for _, invocation := range invocations {
		runtime.toolTurn.activityInvocations[invocation.InvocationID()] = invocation
		event, err := protocol.NewToolReady(activityForInvocation(invocation))
		if err != nil {
			return nil, err
		}
		runtime.toolTurn.activityEmitter(runtime.decorate(event, turnID))
	}
	return runtime.scheduleToolCalls(ctx, turnID, invocations, runtime.toolTurn.activityEmitter)
}

func (runtime *Runtime) executeReadBatch(ctx context.Context, turnID domain.TurnID, invocations []tool.Invocation) ([]tool.InvocationResult, error) {
	type job struct{ index int }
	results := make([]tool.InvocationResult, len(invocations))
	materials := make([]tool.ReadonlyMaterial, len(invocations))
	captures := make([]tool.TextOutputCapture, len(invocations))
	savingReasons := make([]string, len(invocations))
	defer func() {
		for _, capture := range captures {
			if capture != nil {
				_ = capture.Close()
			}
		}
	}()
	started := make([]bool, len(invocations))
	persistCount := len(invocations)
	var admissionErr error
	startedCount := 0
	for index, invocation := range invocations {
		if runtime.toolTurn.resultBudget.BodyExhausted() {
			results[index] = tool.NewInvocationErrorResult(invocation, tool.ResultError, "result_budget_exceeded", "Tool not executed: turn result budget exhausted")
			continue
		}
		admitted, err := runtime.admitToolExecution(ctx, turnID, invocation)
		if err != nil {
			persistCount = index
			admissionErr = err
			break
		}
		if !admitted {
			for next := index; next < len(invocations); next++ {
				results[next] = tool.NewInvocationErrorResult(invocations[next], tool.ResultCancelled, "cancelled", "Tool cancelled")
			}
			break
		}
		started[index] = true
		if invocation.Capability() == tool.CapabilityGlob || invocation.Capability() == tool.CapabilityGrep {
			savingReasons[index] = "storage_unavailable"
			if runtime.textOutputStore != nil {
				capture, err := runtime.textOutputStore.StartTextCapture(ctx, invocation.InvocationID(), invocation.Capability())
				if err == nil {
					captures[index] = capture
				} else if errors.Is(err, tool.ArtifactQuotaExceeded) {
					savingReasons[index] = "quota_exceeded"
				}
			}
		}
		startedCount++
	}
	jobs := make(chan job)
	var workers sync.WaitGroup
	workerCount := startedCount
	if workerCount > maxParallelReadWorkers {
		workerCount = maxParallelReadWorkers
	}
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for current := range jobs {
				invocation := invocations[current.index]
				material, err := runtime.executeInvocation(ctx, invocation, captures[current.index])
				if err != nil {
					result := tool.NewInvocationErrorResult(invocation, tool.ResultError, "invalid_tool_result", "Tool failed: executor returned an invalid result")
					material, _ = tool.NewReadonlyResultMaterial(result)
				}
				materials[current.index] = material
			}
		}()
	}
	for index := range invocations {
		if started[index] {
			jobs <- job{index: index}
		}
	}
	close(jobs)
	workers.Wait()
	for index := 0; index < persistCount; index++ {
		result := results[index]
		observation := tool.UnavailableReadObservation()
		if started[index] {
			allowance, budgetErr := runtime.resultAllowance(invocations[index].Capability())
			if budgetErr != nil {
				return nil, budgetErr
			}
			var err error
			saving, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
			needed := materials[index].SearchSavingNeeded(allowance)
			if captures[index] != nil {
				saving = captures[index].Finish(materials[index].SearchComplete(), needed)
			} else if needed {
				saving, _ = tool.NewArtifactSaving(tool.ArtifactUnavailable, savingReasons[index], tool.ArtifactDescriptor{})
			}
			result, observation, err = materials[index].Finalize(allowance, saving)
			if err != nil || result.Validate() != nil || result.InvocationID() != invocations[index].InvocationID() || result.ProviderCallID() != invocations[index].ProviderCallID() || result.Capability() != invocations[index].Capability() {
				result = tool.NewInvocationErrorResult(invocations[index], tool.ResultError, "invalid_tool_result", "Tool failed: executor returned an invalid result")
				observation = tool.UnavailableReadObservation()
			}
		}
		if result.Validate() != nil {
			return nil, fault.New(fault.CodeStreamProtocol, "tool result slot is invalid")
		}
		if err := runtime.persistToolResult(ctx, turnID, &result); err != nil {
			return nil, err
		}
		results[index] = result
		runtime.readObservations.stage(result, observation)
	}
	if admissionErr != nil {
		return nil, admissionErr
	}
	return results, nil
}

func (runtime *Runtime) executeInvocation(ctx context.Context, invocation tool.Invocation, capture tool.TextOutputCapture) (tool.ReadonlyMaterial, error) {
	switch invocation.Capability() {
	case tool.CapabilitySkill:
		v, _ := invocation.Skill()
		return tool.NewSkillMaterial(runtime.tools.Skill.Execute(ctx, v, append([]domain.SkillAttachment(nil), runtime.explicitSkills...)))
	case tool.CapabilityReadArtifact:
		v, _ := invocation.ReadArtifact()
		return tool.NewArtifactMaterial(runtime.tools.ReadArtifact.Execute(ctx, v))
	case tool.CapabilityRead:
		v, _ := invocation.Read()
		return tool.NewReadMaterial(runtime.tools.Read.Execute(ctx, v))
	case tool.CapabilityGlob:
		v, _ := invocation.Glob()
		return tool.NewSearchMaterial(runtime.tools.Glob.Execute(ctx, v, capture))
	case tool.CapabilityGrep:
		v, _ := invocation.Grep()
		return tool.NewSearchMaterial(runtime.tools.Grep.Execute(ctx, v, capture))
	default:
		return tool.ReadonlyMaterial{}, errors.New("invalid readonly capability")
	}
}

// ResolveToolApproval 由AgentLoop owner恰好一次交付活动批准响应。

func (runtime *Runtime) persistToolResult(ctx context.Context, turnID domain.TurnID, target *tool.InvocationResult) error {
	result, err := runtime.freezeResult(*target)
	if err != nil {
		return err
	}
	*target = result
	if page, ok := result.ArtifactPage(); ok {
		d, exists := runtime.artifactReferences.Lookup(page.ID())
		if !exists || d.ThreadID() != runtime.threadID || uint64(d.SavedBytes()) != page.SavedBytes() || d.Complete() != page.SourceComplete() {
			runtime.poisoned.Store(true)
			return fault.New(fault.CodeStreamProtocol, "artifact page has no matching committed reference")
		}
	}
	if runtime.artifactReferences != nil && runtime.artifactReferences.ValidatePending(result) != nil {
		runtime.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "invalid artifact references")
	}
	for _, d := range result.ArtifactDescriptors() {
		if d.ThreadID() != runtime.threadID || runtime.artifactReferences == nil {
			runtime.poisoned.Store(true)
			return fault.New(fault.CodeStreamProtocol, "artifact thread mismatch")
		}
	}
	draft, err := session.NewToolCallResultDraft(turnID, result)
	if err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "tool result is invalid", err)
	}
	writeContext := context.WithoutCancel(ctx)
	if result.Capability() == tool.CapabilityTaskProgress && result.Status() == tool.ResultSuccess {
		writeContext = ctx
	}
	records, appendErr := runtime.journal.AppendBatch(writeContext, []session.RecordDraft{draft})
	if appendErr != nil {
		if result.Capability() == tool.CapabilityTaskProgress && result.Status() == tool.ResultSuccess && (errors.Is(appendErr, context.Canceled) || errors.Is(appendErr, context.DeadlineExceeded)) && !runtime.journal.Poisoned() {
			call := runtime.toolTurn.toolCalls[result.InvocationID()]
			if call == nil {
				return fault.New(fault.CodeStreamProtocol, "task result has no source")
			}
			*target = tool.NewInvocationErrorResult(call.original, tool.ResultCancelled, "cancelled", "Task update cancelled before commit")
			return runtime.persistToolResult(context.WithoutCancel(ctx), turnID, target)
		}
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist tool result failed", appendErr)
	}
	if len(records) != 1 || records[0].Sequence == 0 {
		runtime.poisoned.Store(true)
		return fault.New(fault.CodeSessionWrite, "tool result has no unique durable sequence")
	}
	if runtime.artifactReferences != nil {
		if err := runtime.artifactReferences.AuthorizeCommitted(result); err != nil {
			runtime.poisoned.Store(true)
			return fault.New(fault.CodeSessionWrite, "artifact authorization failed")
		}
	}
	if err := runtime.commitResultBudget(result); err != nil {
		runtime.poisoned.Store(true)
		return err
	}
	return runtime.notifyToolResult(turnID, result, resultRecordSequence(records))
}

func (runtime *Runtime) commitToolOutputs(ctx context.Context, turnID domain.TurnID, results []tool.InvocationResult) error {
	preparer := runtime.providerOperations.toolOutputs
	if preparer == nil {
		runtime.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "provider does not support tool result preparation")
	}
	outputs := make([]tool.ModelOutput, len(results))
	for i, result := range results {
		evidence, evidenceErr := runtime.resultEvidence(result)
		if evidenceErr != nil {
			return evidenceErr
		}
		var feedback []domain.FrozenHookFeedback
		if state := runtime.hooks.post[result.InvocationID()]; state != nil {
			if state.terminal == nil {
				return fault.New(fault.CodeStreamProtocol, "post hooks are incomplete")
			}
			feedback = postFeedback(state)
		}
		var output tool.ModelOutput
		var err error
		if state := runtime.hooks.post[result.InvocationID()]; state != nil {
			var selection tool.PostHookSelection
			selection, err = tool.NewPostHookSelection(state.plan, state.admission, result)
			if err == nil {
				output, err = tool.NewPostModelOutput(result, evidence, selection, feedback)
			}
		} else {
			output, err = tool.NewModelOutput(result, evidence)
		}
		if err != nil {
			return err
		}
		outputs[i] = output
	}
	prepared, err := preparer.PrepareToolOutputs(outputs)
	if err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "prepare provider tool outputs failed", err)
	}
	envelope, err := prepared.Envelope()
	if err != nil {
		_ = prepared.Discard()
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "provider tool outputs are invalid", err)
	}
	draft, err := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{
		Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload(),
	})
	if err != nil {
		_ = prepared.Discard()
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "provider tool outputs are invalid", err)
	}
	records, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{draft})
	if err != nil {
		_ = prepared.Discard()
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist provider tool outputs failed", err)
	}
	if err := prepared.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "finalize provider tool outputs failed", err)
	}
	if runtime.toolHistory != nil {
		if err := runtime.toolHistory.NativeCommitted(records[0].Sequence, nil); err != nil {
			runtime.poisoned.Store(true)
			return err
		}
	}
	runtime.emitTranscriptCommit(turnID, records[0].Sequence)
	if !runtime.poisoned.Load() {
		runtime.readObservations.activate()
	} else {
		runtime.readObservations.clear()
	}
	return nil
}

func discardPreparedSample(sample *provider.PreparedSample) {
	if sample != nil && !sample.Finalized() {
		_ = sample.Discard()
	}
}

func (runtime *Runtime) failTurn(ctx context.Context, emit Emitter, turnID domain.TurnID, cause error) error {
	code, message, cancelled := failureSummary(cause)
	if !runtime.poisoned.Load() && !runtime.journal.Poisoned() {
		failureDraft, draftErr := session.NewTurnFailedDraft(turnID, session.TurnFailedPayload{
			Code: string(code), Message: message, Cancelled: cancelled,
		})
		if draftErr != nil {
			failure := fault.Wrap(fault.CodeSessionWrite, "build turn failure failed", draftErr)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
		_, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{failureDraft})
		if err != nil {
			failure := fault.Wrap(fault.CodeSessionWrite, "persist turn failure failed", err)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
	}
	runtime.emitFailure(emit, turnID, cause)
	return cause
}

func (runtime *Runtime) decorate(event protocol.Event, turnID domain.TurnID) protocol.Event {
	event.SessionID = runtime.sessionID
	event.ThreadID = runtime.threadID
	event.TurnID = turnID
	return event
}

func (runtime *Runtime) emitFailure(emit Emitter, turnID domain.TurnID, err error) {
	code, message, cancelled := failureSummary(err)
	event, buildErr := protocol.NewTurnFailed(string(code), message, cancelled)
	if runtime.samplePresentation != nil {
		state := runtime.samplePresentation
		event, buildErr = protocol.NewTurnSampleFailed(string(code), message, cancelled, state.resolution)
		runtime.samplePresentation = nil
	}
	if buildErr != nil {
		return
	}
	emit(runtime.decorate(event, turnID))
}

func failureSummary(err error) (fault.Code, string, bool) {
	summary := fault.Project(err)
	return summary.Code, summary.Message, summary.Cancelled
}
