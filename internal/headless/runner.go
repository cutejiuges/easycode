package headless

import (
	"context"
	"io"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// ChatSession 是 headless 宿主使用的最小会话接口。
type ChatSession interface {
	Submit(context.Context, string) (<-chan protocol.Event, error)
	Interrupt()
}

// RunConfig 固定一次 headless 调用的身份与输出。
type RunConfig struct {
	Mode      Mode
	Prompt    string
	SessionID domain.SessionID
	ThreadID  domain.ThreadID
	Resumed   bool
	Output    io.Writer
}

// Result 表示 headless 宿主是否已成功交付或报告终态。
type Result struct {
	Completed         bool
	Failure           fault.Summary
	Reported          bool
	OutputUnavailable bool
}

// Run 提交一个 turn，并等待 RuntimeEvent 流形成唯一终态。
func Run(ctx context.Context, session ChatSession, config RunConfig) Result {
	if ctx == nil {
		return failedResult(fault.New(fault.CodeTurnFailed, "headless context is required"))
	}
	if session == nil || (config.Mode != ModeText && config.Mode != ModeJSON) ||
		strings.TrimSpace(config.Prompt) == "" || config.Output == nil {
		return failedResult(fault.New(fault.CodeTurnFailed, "headless runner is not configured"))
	}
	if err := validateThreadIdentity(config.SessionID, config.ThreadID); err != nil {
		return failedResult(fault.New(fault.CodeStreamProtocol, "headless identity is invalid"))
	}

	var encoder *Encoder
	if config.Mode == ModeJSON {
		encoder = NewEncoder(config.Output)
		started, err := NewThreadStartedEvent(config.SessionID, config.ThreadID, config.Resumed)
		if err != nil {
			return failedResult(fault.New(fault.CodeStreamProtocol, "headless identity is invalid"))
		}
		if err := encoder.Write(started); err != nil {
			return outputFailure()
		}
	}

	events, err := session.Submit(ctx, config.Prompt)
	if err != nil {
		return reportStreamError(encoder, fault.Project(err))
	}
	if events == nil {
		session.Interrupt()
		return reportProtocolError(session, nil, encoder)
	}

	projector := newProjector(config.SessionID, config.ThreadID)
	var text strings.Builder
	var terminal *projection
	cancelChannel := ctx.Done()
	interrupted := false

	for {
		if terminal != nil {
			_, open := <-events
			if !open {
				return deliverTerminal(config.Mode, config.Output, encoder, text.String(), *terminal)
			}
			return reportProtocolError(session, events, encoder)
		}

		select {
		case event, open := <-events:
			if result, done := consumeEvent(session, events, encoder, projector, config.Mode, event, open, &text); done {
				return result.result
			} else if result.terminal != nil {
				terminal = result.terminal
			}
			continue
		default:
		}

		select {
		case event, open := <-events:
			if result, done := consumeEvent(session, events, encoder, projector, config.Mode, event, open, &text); done {
				return result.result
			} else if result.terminal != nil {
				terminal = result.terminal
			}
		case <-cancelChannel:
			if !interrupted {
				session.Interrupt()
				interrupted = true
			}
			cancelChannel = nil
		}
	}
}

type consumeResult struct {
	terminal *projection
	result   Result
}

func consumeEvent(
	session ChatSession,
	events <-chan protocol.Event,
	encoder *Encoder,
	projector *projector,
	mode Mode,
	event protocol.Event,
	open bool,
	text *strings.Builder,
) (consumeResult, bool) {
	if !open {
		return consumeResult{result: reportProtocolError(session, nil, encoder)}, true
	}
	projected, err := projector.project(event)
	if err != nil {
		return consumeResult{result: reportProtocolError(session, events, encoder)}, true
	}
	if mode == ModeText && projected.text != "" {
		text.WriteString(projected.text)
	}
	if projected.terminal {
		copy := projected
		return consumeResult{terminal: &copy}, false
	}
	if encoder != nil {
		if err := encoder.Write(projected.event); err != nil {
			session.Interrupt()
			drain(events)
			return consumeResult{result: outputFailure()}, true
		}
	}
	return consumeResult{}, false
}

func deliverTerminal(
	mode Mode,
	output io.Writer,
	encoder *Encoder,
	text string,
	terminal projection,
) Result {
	if terminal.completed {
		if mode == ModeText {
			content := []byte(text)
			if !strings.HasSuffix(text, "\n") {
				content = append(content, '\n')
			}
			if err := writeFull(output, content); err != nil {
				return outputFailure()
			}
		} else if err := encoder.Write(terminal.event); err != nil {
			return outputFailure()
		}
		return Result{Completed: true}
	}
	if encoder == nil {
		return Result{Failure: terminal.failure}
	}
	if err := encoder.Write(terminal.event); err != nil {
		return outputFailure()
	}
	return Result{Failure: terminal.failure, Reported: true}
}

func reportProtocolError(session ChatSession, events <-chan protocol.Event, encoder *Encoder) Result {
	if events != nil {
		session.Interrupt()
		drain(events)
	}
	return reportStreamError(
		encoder,
		fault.Summary{Code: fault.CodeStreamProtocol, Message: "runtime event stream is invalid"},
	)
}

func reportStreamError(encoder *Encoder, failure fault.Summary) Result {
	if encoder == nil {
		return Result{Failure: failure}
	}
	event, err := NewErrorEvent(failure)
	if err != nil || encoder.Write(event) != nil {
		return outputFailure()
	}
	return Result{Failure: failure, Reported: true}
}

func failedResult(err error) Result {
	return Result{Failure: fault.Project(err)}
}

func outputFailure() Result {
	return Result{
		Failure:           fault.Summary{Code: fault.CodeOutput, Message: "write headless output failed"},
		OutputUnavailable: true,
	}
}

func drain(events <-chan protocol.Event) {
	if events == nil {
		return
	}
	for range events {
	}
}
