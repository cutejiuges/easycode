package runtime

import (
	"context"
	"sync/atomic"

	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// ResumePermit 将一次交接绑定到源控制实例，保持到释放获确认或源关闭。
type ResumePermit struct {
	source *ChatSession
	held   atomic.Bool
}

// Valid 报告此许可是否仍由健康的源控制实例持有。
func (p *ResumePermit) Valid() bool {
	return p != nil && p.held.Load() && p.source.Live()
}

// Release 在交接未被接受时，等待 owner 确认释放此许可。
func (p *ResumePermit) Release(ctx context.Context) error {
	if p == nil || p.source == nil {
		return nil
	}
	if p.held.Load() {
		if err := p.source.loop.requestResume(ctx, p, true); err != nil {
			return err
		}
	}
	p.source.clearResumeAdmission(p)
	return nil
}

// Live 报告交互控制实例已启动且未进入终态，供宿主核对候选生命周期。
func (session *ChatSession) Live() bool {
	session.mu.Lock()
	live, loop, cancelled := session.started && !session.closed && session.interactive, session.loop, session.runCancelled
	session.mu.Unlock()
	return live && resumeSignalClosed(loop.ready) && !resumeSignalClosed(cancelled) &&
		loop.state.Load() == agentLoopRunning && !session.runtime.poisoned.Load()
}

// ReserveResume 在处理输入或启动交接之间确定唯一顺序。
func (session *ChatSession) ReserveResume(ctx context.Context) (*ResumePermit, error) {
	if ctx == nil {
		return nil, fault.New(fault.CodeInvalidInput, "resume context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fault.Wrap(fault.CodeUserCancelled, "resume admission was cancelled", err)
	}
	permit := &ResumePermit{source: session}
	session.mu.Lock()
	if !session.started || session.closed || !session.interactive {
		session.mu.Unlock()
		return nil, fault.New(fault.CodeTurnFailed, "interactive session is not running")
	}
	if session.resumePermit != nil || session.appendAdmissions != 0 {
		session.mu.Unlock()
		return nil, fault.New(fault.CodeSessionBusy, "session is not idle for resume")
	}
	session.resumePermit = permit
	loop := session.loop
	session.mu.Unlock()
	if err := loop.requestResume(ctx, permit, false); err != nil {
		session.clearResumeAdmission(permit)
		return nil, err
	}
	return permit, nil
}

func (session *ChatSession) beginAppend() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.resumePermit != nil {
		return fault.New(fault.CodeSessionBusy, "session is reserved for resume")
	}
	session.appendAdmissions++
	return nil
}

func (session *ChatSession) finishAppend() {
	session.mu.Lock()
	session.appendAdmissions--
	session.mu.Unlock()
}

func (session *ChatSession) clearResumeAdmission(permit *ResumePermit) {
	session.mu.Lock()
	if session.resumePermit == permit {
		session.resumePermit = nil
	}
	session.mu.Unlock()
}

type resumeRequest struct {
	permit    *ResumePermit
	release   bool
	cancelled <-chan struct{}
	result    chan error
}

func (loop *AgentLoop) requestResume(ctx context.Context, permit *ResumePermit, release bool) error {
	if ctx == nil {
		return fault.New(fault.CodeInvalidInput, "resume context is required")
	}
	if err := ctx.Err(); err != nil {
		return fault.Wrap(fault.CodeUserCancelled, "resume admission was cancelled", err)
	}
	request := resumeRequest{permit: permit, release: release, cancelled: ctx.Done(), result: make(chan error, 1)}
	select {
	case loop.resume <- request:
		// owner 必须返回确定结果，即使调用方已取消等待。
		return <-request.result
	case <-ctx.Done():
		return fault.Wrap(fault.CodeUserCancelled, "resume admission was cancelled", ctx.Err())
	case <-loop.done:
		if release {
			return nil
		}
		return fault.New(fault.CodeTurnFailed, "interactive session is not running")
	}
}

func (state *agentLoopRunState) handleResume(request resumeRequest) error {
	if request.release {
		if state.resumePermit == request.permit {
			state.clearResumePermit()
		}
		return nil
	}
	if state.mode != loopModeAccepting || state.loop.runtime.poisoned.Load() || state.loop.runtime.journal.Poisoned() {
		return fault.New(fault.CodeTurnFailed, "interactive session is not available")
	}
	if state.resumePermit != nil || state.active != nil || state.compaction != nil || len(state.queue) != 0 {
		return fault.New(fault.CodeSessionBusy, "session is not idle for resume")
	}
	// 接收点只检查完成信号并安装许可。未获接收的请求不会留下许可。
	if resumeSignalClosed(request.cancelled) || resumeSignalClosed(state.runContextDone) {
		return fault.Wrap(fault.CodeUserCancelled, "resume admission was cancelled", context.Canceled)
	}
	state.resumePermit = request.permit
	state.resumePermit.held.Store(true)
	return nil
}

func (state *agentLoopRunState) clearResumePermit() {
	if state.resumePermit != nil {
		state.resumePermit.held.Store(false)
		state.resumePermit = nil
	}
}

func allowsCommandDuringResume(kind protocol.CommandKind) bool {
	switch kind {
	case protocol.CommandQueryCache, protocol.CommandQueryContext, protocol.CommandQueryEffort, protocol.CommandQueryTasks, protocol.CommandShutdown:
		return true
	default:
		return false
	}
}

func resumeSignalClosed(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	default:
		return false
	}
}
