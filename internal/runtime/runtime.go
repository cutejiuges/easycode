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
	SessionID       domain.SessionID
	ThreadID        domain.ThreadID
	Journal         Journal
	GenerateTurnID  TurnIDGenerator
	InterruptedTail bool
	ContextProfile  contextplan.ProviderProfile
	ContextBudget   contextplan.Budget
	ContextPlanner  ContextPlanner
}

// Runtime 持有一条会话级 Provider Conversation 和同一 thread journal。
type Runtime struct {
	conversation provider.Conversation
	journal      Journal
	sessionID    domain.SessionID
	threadID     domain.ThreadID
	newTurnID    TurnIDGenerator
	profile      contextplan.ProviderProfile
	budget       contextplan.Budget
	planner      ContextPlanner
	active       atomic.Bool
	poisoned     atomic.Bool
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
		profile: config.ContextProfile, budget: config.ContextBudget, planner: config.ContextPlanner,
	}, nil
}

// RunTurn 执行一次文本 turn，并等待 Provider terminal 后的 durable 收口。
func (runtime *Runtime) RunTurn(
	ctx context.Context,
	input provider.TurnInput,
	emit Emitter,
) error {
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
	if !runtime.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	defer runtime.active.Store(false)

	turnID, err := runtime.newTurnID()
	if err != nil || !turnID.Valid() {
		if err == nil {
			err = fmt.Errorf("generated turn ID is invalid")
		}
		failure := fault.Wrap(fault.CodeTurnFailed, "turn identity generation failed", err)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	startedDraft, err := session.NewTurnStartedDraft(turnID)
	if err != nil {
		failure := fault.Wrap(fault.CodeTurnFailed, "build turn start failed", err)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	if _, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{startedDraft}); err != nil {
		failure := fault.Wrap(fault.CodeSessionWrite, "persist turn start failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	emit(runtime.decorate(protocol.NewTurnStarted(), turnID))
	planInput, err := runtime.contextPlanningInput(input)
	if err != nil {
		failure := fault.New(fault.CodeTurnFailed, "context planning input is invalid")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	plan, err := runtime.planner.Plan(planInput)
	if err != nil {
		failure := fault.New(fault.CodeTurnFailed, "context planning failed")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}
	if plan.Decision().State() == contextplan.BudgetOverLimit {
		total, _ := plan.TotalEstimate().Tokens()
		limit, _ := plan.Decision().EffectiveLimit()
		failure := fault.New(
			fault.CodeContextLimitExceeded,
			fmt.Sprintf("estimated context tokens %d exceed effective input limit %d", total, limit),
		)
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, failure)
	}

	stream, err := runtime.conversation.Stream(ctx, input)
	if err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}

	var terminal *provider.StreamEvent
	for event := range stream {
		if terminal != nil {
			err = fault.New(fault.CodeStreamProtocol, "provider emitted an event after terminal")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}

		switch event.Kind {
		case provider.StreamEventSemantic:
			if ctx.Err() == nil && event.Event.Kind != "" {
				emit(runtime.decorate(event.Event, turnID))
			}
		case provider.StreamEventNative:
			// Provider 原生 item 只由 Conversation staging 持有，Runtime 不解释 wire。
		case provider.StreamEventCompleted, provider.StreamEventFailed, provider.StreamEventCancelled:
			copy := event
			terminal = &copy
		default:
			err = fault.New(fault.CodeStreamProtocol, "provider emitted an invalid stream event")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
	}

	if terminal == nil {
		err = fault.New(fault.CodeStreamProtocol, "provider stream closed before terminal")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}

	switch terminal.Kind {
	case provider.StreamEventCompleted:
		if terminal.Prepared == nil || terminal.Prepared.Finalized() {
			err = fault.New(fault.CodeStreamProtocol, "provider completed without a fresh prepared sample")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		envelope, envelopeErr := terminal.Prepared.Envelope()
		if envelopeErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", envelopeErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		envelope, envelopeErr = envelope.Clone()
		if envelopeErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", envelopeErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		usage, usageErr := terminal.Prepared.Usage()
		if usageErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "provider prepared usage is invalid", usageErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		turnUsage, aggregateErr := domain.AggregateSampleUsage([]domain.SampleUsage{usage})
		if aggregateErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "turn usage is invalid", aggregateErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		commitDraft, draftErr := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{
			Provider: envelope.Family(), Wire: envelope.Wire(),
			PayloadVersion: envelope.PayloadVersion(), Payload: envelope.Payload(),
		})
		if draftErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "provider native commit is invalid", draftErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		usageDraft, draftErr := session.NewSampleUsageDraft(turnID, usage)
		if draftErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "sample usage is invalid", draftErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		completedDraft, draftErr := session.NewTurnCompletedDraft(turnID)
		if draftErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "turn completion is invalid", draftErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		completedEvent, eventErr := protocol.NewTurnCompleted(turnUsage)
		if eventErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "turn completion is invalid", eventErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		_, err = runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{commitDraft, usageDraft, completedDraft})
		if err != nil {
			failure := fault.Wrap(fault.CodeSessionWrite, "persist completed turn failed", err)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
		if err := terminal.Prepared.Finalize(); err != nil {
			failure := fault.Wrap(fault.CodeStreamProtocol, "finalize prepared sample failed", err)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
		emit(runtime.decorate(completedEvent, turnID))
		return nil
	case provider.StreamEventCancelled:
		err = terminal.Err
		if err == nil || errors.Is(err, context.Canceled) {
			err = fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	case provider.StreamEventFailed:
		err = terminal.Err
		if err == nil {
			err = fault.New(fault.CodeProviderRequest, "provider request failed")
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	default:
		err = fault.New(fault.CodeStreamProtocol, "provider terminal is invalid")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
}

func (runtime *Runtime) contextPlanningInput(input provider.TurnInput) (contextplan.PlanningInput, error) {
	history := runtime.conversation.ProjectHistory()
	footprint, err := runtime.conversation.HistoryFootprint()
	if err != nil {
		return contextplan.PlanningInput{}, err
	}
	return contextplan.NewPlanningInput(runtime.profile, history, footprint, input.Text, runtime.budget)
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
