// Package runtime 实现共享 turn 模板和 durable 生命周期编排。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

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

// ContextPlanner 是 Runtime 在 Provider 副作用前使用的纯内存规划能力。
type ContextPlanner interface {
	Plan(contextplan.PlanningInput) (contextplan.ContextPlan, error)
}

// Config 固定一个 Session-bound Runtime 的身份与 durable 边界。
type Config struct {
	SessionID            domain.SessionID
	ThreadID             domain.ThreadID
	Journal              Journal
	GenerateTurnID       TurnIDGenerator
	GenerateInvocationID InvocationIDGenerator
	InterruptedTail      bool
	ContextProfile       contextplan.ProviderProfile
	ToolCatalog          tool.CatalogSnapshot
	ReadExecutor         tool.ReadExecutor
	GlobExecutor         tool.GlobExecutor
	GrepExecutor         tool.GrepExecutor
	ProjectInstructions  domain.ProjectInstructionsSnapshot
	ContextBudget        contextplan.Budget
	ContextPlanner       ContextPlanner
}

// Runtime 持有一条会话级 Provider Conversation 和同一 thread journal。
type Runtime struct {
	conversation        provider.Conversation
	journal             Journal
	sessionID           domain.SessionID
	threadID            domain.ThreadID
	newTurnID           TurnIDGenerator
	newInvocationID     InvocationIDGenerator
	profile             contextplan.ProviderProfile
	toolCatalog         tool.CatalogSnapshot
	readExecutor        tool.ReadExecutor
	globExecutor        tool.GlobExecutor
	grepExecutor        tool.GrepExecutor
	toolPolicy          tool.ReadOnlyPolicy
	projectInstructions domain.ProjectInstructionsSnapshot
	budget              contextplan.Budget
	planner             ContextPlanner
	active              atomic.Bool
	poisoned            atomic.Bool
}

// New 创建已绑定稳定 Session 身份的共享 Runtime。
func New(conversation provider.Conversation, config Config) (*Runtime, error) {
	if conversation == nil {
		return nil, fault.New(fault.CodeProviderUnavailable, "provider conversation is not configured")
	}
	if !config.SessionID.Valid() || !config.ThreadID.Valid() {
		return nil, fault.New(fault.CodeTurnFailed, "runtime session identity is invalid")
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
	if config.ContextProfile.Family() != conversation.Family() {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime context profile provider does not match conversation")
	}
	if err := config.ToolCatalog.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime tool catalog is invalid", err)
	}
	if _, err := config.ToolCatalog.View(conversation.Family()); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime tool catalog Provider view is invalid", err)
	}
	if config.ReadExecutor == nil || config.GlobExecutor == nil || config.GrepExecutor == nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime read-only executors are not configured")
	}
	projectInstructions, err := config.ProjectInstructions.Clone()
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime project instructions are invalid", err)
	}
	if err := config.ContextBudget.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "runtime context budget is invalid", err)
	}
	if config.ContextPlanner == nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "runtime context planner is not configured")
	}
	if config.GenerateTurnID == nil {
		config.GenerateTurnID = domain.GenerateTurnID
	}
	if config.GenerateInvocationID == nil {
		config.GenerateInvocationID = tool.GenerateInvocationID
	}
	return &Runtime{
		conversation: conversation, journal: config.Journal,
		sessionID: config.SessionID, threadID: config.ThreadID, newTurnID: config.GenerateTurnID,
		newInvocationID: config.GenerateInvocationID,
		profile:         config.ContextProfile, toolCatalog: config.ToolCatalog.Clone(),
		readExecutor: config.ReadExecutor, globExecutor: config.GlobExecutor, grepExecutor: config.GrepExecutor,
		toolPolicy: tool.NewReadOnlyPolicy(), projectInstructions: projectInstructions,
		budget: config.ContextBudget, planner: config.ContextPlanner,
	}, nil
}

// RunTurn 执行一次文本 turn，并等待 Provider terminal 后的 durable 收口。
func (runtime *Runtime) RunTurn(
	ctx context.Context,
	input provider.TurnInput,
	emit Emitter,
) error {
	if err := runtime.validateTurnRequest(ctx, emit); err != nil {
		return err
	}
	if !runtime.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	defer runtime.active.Store(false)

	turnID, err := runtime.beginTurn(ctx, emit)
	if err != nil {
		return err
	}
	currentInput := input
	usages := make([]domain.SampleUsage, 0, maxSamplesPerTurn)
	totalCalls := 0
	seenInvocationIDs := make(map[tool.InvocationID]struct{})
	for sampleIndex := 0; ; sampleIndex++ {
		if sampleIndex >= maxSamplesPerTurn {
			failure := fault.New(fault.CodeTurnFailed, "tool_loop_limit_exceeded")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
		}
		terminal, sampleErr := runtime.runProviderSample(ctx, emit, turnID, currentInput)
		if sampleErr != nil {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, sampleErr)
		}
		if terminal.Kind() != provider.StreamEventCompleted {
			return runtime.finishFailedProviderStream(ctx, emit, turnID, terminal)
		}
		sample := terminal.PreparedSample()
		ready, usage, inspectErr := inspectPreparedSample(sample)
		if inspectErr != nil {
			discardPreparedSample(sample)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, inspectErr)
		}
		if len(ready) == 0 {
			usages = append(usages, usage)
			return runtime.commitFinalSample(ctx, emit, turnID, sample, usages)
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
		results, executeErr := runtime.executeToolCalls(ctx, turnID, invocations)
		if executeErr != nil {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, executeErr)
		}
		if outputErr := runtime.commitToolOutputs(ctx, turnID, results); outputErr != nil {
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, outputErr)
		}
		if ctx.Err() != nil {
			cancelled := fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, cancelled)
		}
		currentInput = provider.NewToolContinuationInput()
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

	reconcileContext := context.WithoutCancel(ctx)
	calls := plan.Calls()
	results := make([]tool.InvocationResult, 0, len(calls))
	outcomeUncertain := false
	for _, call := range calls {
		var result tool.InvocationResult
		switch call.State {
		case session.ReplayedToolCallReady:
			result = tool.NewInvocationErrorResult(
				call.Invocation, tool.ResultCancelled, "session_interrupted_before_execution",
				"Tool cancelled: session interrupted before execution",
			)
			if err := runtime.persistToolResult(reconcileContext, plan.TurnID(), result); err != nil {
				return err
			}
		case session.ReplayedToolCallStarted:
			result = tool.NewInvocationErrorResult(
				call.Invocation, tool.ResultOutcomeUncertain, "outcome_uncertain",
				"Tool failed: execution outcome is uncertain",
			)
			if err := runtime.persistToolResult(reconcileContext, plan.TurnID(), result); err != nil {
				return err
			}
			outcomeUncertain = true
		case session.ReplayedToolCallResult:
			result = call.Result
			outcomeUncertain = outcomeUncertain || result.Status() == tool.ResultOutcomeUncertain
		default:
			return fault.New(fault.CodeSessionCorruption, "tool recovery call state is invalid")
		}
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

func (runtime *Runtime) planTurnInput(input provider.TurnInput) (provider.TurnInput, error) {
	planInput, err := runtime.contextPlanningInput(input)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "context planning input is invalid")
	}
	plan, err := runtime.planner.Plan(planInput)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "context planning failed")
	}
	if plan.Decision().State() == contextplan.BudgetOverLimit {
		total, _ := plan.TotalEstimate().Tokens()
		limit, _ := plan.Decision().EffectiveLimit()
		return provider.TurnInput{}, fault.New(
			fault.CodeContextLimitExceeded,
			fmt.Sprintf("estimated context tokens %d exceed effective input limit %d", total, limit),
		)
	}
	providerInput := provider.TurnInput{Text: input.Text}
	if input.IsToolContinuation() {
		providerInput = provider.NewToolContinuationInput()
	}
	providerInput, err = providerInput.WithProjectInstructions(runtime.projectInstructions)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "provider input is invalid")
	}
	providerInput, err = providerInput.WithToolCatalog(runtime.toolCatalog)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "provider tool catalog is invalid")
	}
	return providerInput, nil
}

func (runtime *Runtime) consumeProviderStream(
	ctx context.Context,
	cancel context.CancelFunc,
	stream <-chan provider.StreamEvent,
	emit Emitter,
	turnID domain.TurnID,
) (provider.StreamEvent, error) {
	var terminal provider.StreamEvent
	terminalSeen := false
	var firstErr error

	for event := range stream {
		if firstErr != nil {
			continue
		}
		if err := event.Validate(); err != nil {
			firstErr = fault.Wrap(fault.CodeStreamProtocol, "provider emitted an invalid stream event", err)
			cancel()
			continue
		}
		if terminalSeen {
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
			if ctx.Err() == nil {
				emit(runtime.decorate(event.Semantic(), turnID))
			}
		case provider.StreamEventNative:
			// Provider 原生 item 只由 Conversation staging 持有，Runtime 不解释 wire。
		}
	}

	if firstErr != nil {
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
	input provider.TurnInput,
) (provider.StreamEvent, error) {
	providerInput, err := runtime.planTurnInput(input)
	if err != nil {
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
		PayloadVersion: envelope.PayloadVersion(), Payload: envelope.Payload(),
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
	if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{commitDraft, usageDraft, completedDraft}); err != nil {
		discardPreparedSample(sample)
		failure := fault.Wrap(fault.CodeSessionWrite, "persist completed turn failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	if err := sample.Finalize(); err != nil {
		failure := fault.Wrap(fault.CodeStreamProtocol, "finalize prepared sample failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
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
	envelope, err := sample.Envelope()
	if err != nil {
		discardPreparedSample(sample)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", err)
	}
	commitDraft, err := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{
		Provider: envelope.Family(), Wire: envelope.Wire(), PayloadVersion: envelope.PayloadVersion(), Payload: envelope.Payload(),
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
		if runtime.toolPolicy.Decide(call) != tool.PolicyAllow {
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
	if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), drafts); err != nil {
		discardPreparedSample(sample)
		runtime.poisoned.Store(true)
		return nil, fault.Wrap(fault.CodeSessionWrite, "persist tool call sample failed", err)
	}
	if err := sample.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		return nil, fault.Wrap(fault.CodeStreamProtocol, "finalize prepared sample failed", err)
	}
	return invocations, nil
}

func (runtime *Runtime) executeToolCalls(ctx context.Context, turnID domain.TurnID, invocations []tool.Invocation) ([]tool.InvocationResult, error) {
	type job struct{ index int }
	results := make([]tool.InvocationResult, len(invocations))
	started := make([]bool, len(invocations))
	persistCount := len(invocations)
	var admissionErr error
	startedCount := 0
	for index, invocation := range invocations {
		if ctx.Err() != nil {
			results[index] = tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled", "Tool cancelled")
			continue
		}
		startedDraft, err := session.NewToolExecutionStartedDraft(turnID, invocation.InvocationID())
		if err != nil {
			return nil, fault.Wrap(fault.CodeStreamProtocol, "tool execution start is invalid", err)
		}
		if _, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{startedDraft}); err != nil {
			if errors.Is(err, context.Canceled) && !runtime.journal.Poisoned() {
				results[index] = tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled", "Tool cancelled")
				continue
			}
			if runtime.journal.Poisoned() {
				runtime.poisoned.Store(true)
			}
			persistCount = index
			admissionErr = fault.Wrap(fault.CodeSessionWrite, "persist tool execution start failed", err)
			break
		}
		started[index] = true
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
				result := runtime.executeInvocation(ctx, invocation)
				if result.Validate() != nil || result.InvocationID() != invocation.InvocationID() ||
					result.ProviderCallID() != invocation.ProviderCallID() || result.Capability() != invocation.Capability() {
					result = tool.NewInvocationErrorResult(invocation, tool.ResultError, "invalid_tool_result", "Tool failed: executor returned an invalid result")
				}
				results[current.index] = result
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
		if result.Validate() != nil {
			return nil, fault.New(fault.CodeStreamProtocol, "tool result slot is invalid")
		}
		if err := runtime.persistToolResult(ctx, turnID, result); err != nil {
			return nil, err
		}
		results[index] = result
	}
	if admissionErr != nil {
		return nil, admissionErr
	}
	return results, nil
}

func (runtime *Runtime) executeInvocation(ctx context.Context, invocation tool.Invocation) tool.InvocationResult {
	switch invocation.Capability() {
	case tool.CapabilityRead:
		value, _ := invocation.Read()
		return runtime.readExecutor.Execute(ctx, value)
	case tool.CapabilityGlob:
		value, _ := invocation.Glob()
		return runtime.globExecutor.Execute(ctx, value)
	case tool.CapabilityGrep:
		value, _ := invocation.Grep()
		return runtime.grepExecutor.Execute(ctx, value)
	default:
		return tool.InvocationResult{}
	}
}

func (runtime *Runtime) persistToolResult(ctx context.Context, turnID domain.TurnID, result tool.InvocationResult) error {
	draft, err := session.NewToolCallResultDraft(turnID, result)
	if err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "tool result is invalid", err)
	}
	if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{draft}); err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist tool result failed", err)
	}
	return nil
}

func (runtime *Runtime) commitToolOutputs(ctx context.Context, turnID domain.TurnID, results []tool.InvocationResult) error {
	preparer, ok := runtime.conversation.(provider.ToolResultPreparer)
	if !ok {
		runtime.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "provider does not support tool result preparation")
	}
	prepared, err := preparer.PrepareToolOutputs(results)
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
		Provider: envelope.Family(), Wire: envelope.Wire(), PayloadVersion: envelope.PayloadVersion(), Payload: envelope.Payload(),
	})
	if err != nil {
		_ = prepared.Discard()
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "provider tool outputs are invalid", err)
	}
	if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{draft}); err != nil {
		_ = prepared.Discard()
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist provider tool outputs failed", err)
	}
	if err := prepared.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeStreamProtocol, "finalize provider tool outputs failed", err)
	}
	return nil
}

func discardPreparedSample(sample *provider.PreparedSample) {
	if sample != nil && !sample.Finalized() {
		_ = sample.Discard()
	}
}

func (runtime *Runtime) contextPlanningInput(input provider.TurnInput) (contextplan.PlanningInput, error) {
	history := runtime.conversation.ProjectHistory()
	footprint, err := runtime.conversation.HistoryFootprint()
	if err != nil {
		return contextplan.PlanningInput{}, err
	}
	currentInput := contextplan.NewUserTextInput(input.Text)
	if input.IsToolContinuation() {
		currentInput = contextplan.NewToolContinuationInput()
	}
	return contextplan.NewPlanningInput(
		runtime.profile, runtime.toolCatalog, runtime.projectInstructions, history, footprint,
		currentInput, runtime.budget,
	)
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
	if buildErr != nil {
		return
	}
	emit(runtime.decorate(event, turnID))
}

func failureSummary(err error) (fault.Code, string, bool) {
	summary := fault.Project(err)
	return summary.Code, summary.Message, summary.Cancelled
}
