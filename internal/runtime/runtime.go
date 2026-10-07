// Package runtime 实现共享 turn 模板和 durable 生命周期编排。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
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

// ContextPlanner 是 Runtime 在 Provider 副作用前使用的纯内存规划能力。
type ContextPlanner interface {
	Plan(contextplan.PlanningInput) (contextplan.ContextPlan, error)
}

// Config 固定一个 Session-bound Runtime 的身份与 durable 边界。
type Config struct {
	SessionID           domain.SessionID
	ThreadID            domain.ThreadID
	Journal             Journal
	GenerateTurnID      TurnIDGenerator
	InterruptedTail     bool
	ContextProfile      contextplan.ProviderProfile
	ProjectInstructions domain.ProjectInstructionsSnapshot
	ContextBudget       contextplan.Budget
	ContextPlanner      ContextPlanner
}

// Runtime 持有一条会话级 Provider Conversation 和同一 thread journal。
type Runtime struct {
	conversation        provider.Conversation
	journal             Journal
	sessionID           domain.SessionID
	threadID            domain.ThreadID
	newTurnID           TurnIDGenerator
	profile             contextplan.ProviderProfile
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
	return &Runtime{
		conversation: conversation, journal: config.Journal,
		sessionID: config.SessionID, threadID: config.ThreadID, newTurnID: config.GenerateTurnID,
		profile: config.ContextProfile, projectInstructions: projectInstructions,
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
	providerInput, err := runtime.planTurnInput(input)
	if err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}

	streamContext, cancelStream := context.WithCancel(ctx)
	stream, err := runtime.conversation.Stream(streamContext, providerInput)
	if err != nil {
		cancelStream()
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
	if stream == nil {
		cancelStream()
		failure := fault.New(fault.CodeStreamProtocol, "provider returned a nil stream")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}

	terminal, err := runtime.consumeProviderStream(ctx, cancelStream, stream, emit, turnID)
	cancelStream()
	if err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
	return runtime.finishProviderStream(ctx, emit, turnID, terminal)
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
	providerInput, err := (provider.TurnInput{Text: input.Text}).WithProjectInstructions(runtime.projectInstructions)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "provider input is invalid")
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

func (runtime *Runtime) finishProviderStream(
	ctx context.Context,
	emit Emitter,
	turnID domain.TurnID,
	terminal provider.StreamEvent,
) error {
	switch terminal.Kind() {
	case provider.StreamEventCompleted:
		return runtime.commitCompletedTurn(ctx, emit, turnID, terminal.PreparedSample())
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

func (runtime *Runtime) commitCompletedTurn(
	ctx context.Context,
	emit Emitter,
	turnID domain.TurnID,
	sample *provider.PreparedSample,
) error {
	if sample == nil || sample.Finalized() {
		failure := fault.New(fault.CodeStreamProtocol, "provider completed without a fresh prepared sample")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
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
	turnUsage, err := domain.AggregateSampleUsage([]domain.SampleUsage{usage})
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

func (runtime *Runtime) contextPlanningInput(input provider.TurnInput) (contextplan.PlanningInput, error) {
	history := runtime.conversation.ProjectHistory()
	footprint, err := runtime.conversation.HistoryFootprint()
	if err != nil {
		return contextplan.PlanningInput{}, err
	}
	return contextplan.NewPlanningInput(
		runtime.profile, runtime.projectInstructions, history, footprint, input.Text, runtime.budget,
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
