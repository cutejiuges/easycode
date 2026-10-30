package builtin

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"easycode/internal/tool"
)

// processHandle 封装一次启动的进程组；实现不得向上层泄漏PID。
type processHandle interface {
	stdout() io.ReadCloser
	stderr() io.ReadCloser
	observeExit() error
	wait() (int, error)
	terminate(force bool) error
	groupAlive() (bool, error)
}

type processLauncher interface {
	start(context.Context, tool.ExecCandidate) (processHandle, error)
}

type processTimer interface {
	channel() <-chan time.Time
	stop()
}

type processClock interface {
	now() time.Time
	timer(time.Duration) processTimer
}

type wallProcessClock struct{}
type wallProcessTimer struct{ timerValue *time.Timer }

func (wallProcessClock) now() time.Time { return time.Now() }
func (wallProcessClock) timer(duration time.Duration) processTimer {
	return wallProcessTimer{timerValue: time.NewTimer(duration)}
}
func (timer wallProcessTimer) channel() <-chan time.Time { return timer.timerValue.C }
func (timer wallProcessTimer) stop()                     { timer.timerValue.Stop() }

// ProcessExecution 是一次命令的唯一owner；Run串行执行且只能接收一次。
type ProcessExecution struct {
	launcher    processLauncher
	clock       processClock
	candidate   tool.ExecCandidate
	renderer    tool.ExecOutputRenderer
	mu          sync.Mutex
	accepted    bool
	done        chan struct{}
	progress    *processProgress
	outputStore tool.CommandOutputStore
}

func newProcessExecution(launcher processLauncher, clock processClock, candidate tool.ExecCandidate, renderer tool.ExecOutputRenderer) (*ProcessExecution, error) {
	if launcher == nil || clock == nil || candidate.Validate() != nil || renderer == nil {
		return nil, errors.New("invalid process execution configuration")
	}
	return &ProcessExecution{launcher: launcher, clock: clock, candidate: candidate, renderer: renderer, done: make(chan struct{}), progress: newProcessProgress()}, nil
}

// Done 只在owner的进程等待和所有reader完成后关闭。
func (execution *ProcessExecution) Done() <-chan struct{} { return execution.done }

// Notifications 通知宿主读取最新快照；慢消费者只会合并中间帧。
func (execution *ProcessExecution) Notifications() <-chan struct{} { return execution.progress.notify }

// Progress 返回已存储的不可变快照，不读取时钟或执行I/O。
func (execution *ProcessExecution) Progress() tool.ExecProgress { return execution.progress.snapshot() }

type processWaitResult struct {
	exit int
	err  error
}
type processReadResult struct {
	output tool.ExecOutputSnapshot
	err    error
}

// Run 接收一次执行，取消后仍持有所有goroutine并等待清理。
func (execution *ProcessExecution) Run(ctx context.Context) tool.InvocationResult {
	execution.mu.Lock()
	if execution.accepted {
		execution.mu.Unlock()
		return tool.NewExecErrorResult(execution.candidate.Invocation(), tool.ResultError, "execution_already_accepted", "Exec failed: execution_already_accepted")
	}
	execution.accepted = true
	execution.mu.Unlock()
	defer close(execution.done)
	defer close(execution.progress.notify)
	failed := func(status tool.ResultStatus, code string) tool.InvocationResult {
		return tool.NewExecErrorResult(execution.candidate.Invocation(), status, code, "Exec failed: "+code)
	}
	if ctx == nil || ctx.Err() != nil {
		return failed(tool.ResultCancelled, "cancelled")
	}
	started := execution.clock.now()
	var capture tool.CommandOutputCapture
	var captureErr error
	if execution.outputStore != nil {
		capture, captureErr = execution.outputStore.StartCapture(ctx, execution.candidate.Invocation().InvocationID())
		if capture != nil {
			defer func() { _ = capture.Close() }()
		}
	}
	process, err := execution.launcher.start(ctx, execution.candidate)
	if err != nil {
		if errors.Is(err, errExecUnsupported) {
			return failed(tool.ResultError, "unsupported_platform")
		}
		if errors.Is(err, errSandboxUnavailable) {
			return failed(tool.ResultError, "sandbox_unavailable")
		}
		return failed(tool.ResultError, "process_start_failed")
	}
	stdout, stderr := process.stdout(), process.stderr()
	outDone, errDone := make(chan processReadResult, 1), make(chan processReadResult, 1)
	// reader只交付一次结果；channel由创建它的owner在接收后关闭。
	read := func(source io.ReadCloser, target chan processReadResult, isStderr bool) {
		writer := processOutputWriter{progress: execution.progress, stderr: isStderr, clock: execution.clock, started: started}
		if capture != nil {
			if isStderr {
				writer.save = capture.WriteStderr
			} else {
				writer.save = capture.WriteStdout
			}
		}
		_, readErr := io.Copy(&writer, source)
		target <- processReadResult{output: writer.buffer.Snapshot(), err: readErr}
	}
	go read(stdout, outDone, false)
	go read(stderr, errDone, true)
	exitDone := make(chan error, 1)
	go func() {
		exitDone <- process.observeExit()
	}()
	timeout := execution.clock.timer(time.Duration(execution.candidate.Invocation().Input().TimeoutMillis()) * time.Millisecond)
	defer timeout.stop()
	observed := false
	cancelled, timedOut, uncertain := false, false, false
	select {
	case err := <-exitDone:
		observed, uncertain = true, err != nil
	case <-ctx.Done():
		cancelled = true
	case <-timeout.channel():
		timedOut = true
	}
	// 即使leader正常退出，也必须处理原组的残留成员。
	alive, probeErr := process.groupAlive()
	uncertain = uncertain || probeErr != nil
	if cancelled || timedOut || alive || uncertain {
		if err := process.terminate(false); err != nil {
			uncertain = true
		}
		grace := execution.clock.timer(250 * time.Millisecond)
		<-grace.channel()
		grace.stop()
		alive, probeErr = process.groupAlive()
		uncertain = uncertain || probeErr != nil
		// TERM已清空活动组时不再向仅含僵尸的组发送KILL。
		if alive || probeErr != nil {
			if err := process.terminate(true); err != nil {
				uncertain = true
			}
		}
	}
	if !observed {
		uncertain = (<-exitDone) != nil || uncertain
	}
	close(exitDone)
	// 此时leader尚未回收，组编号不能复用；给已发出的KILL有界收口时间。
	for attempt := 0; ; attempt++ {
		alive, probeErr = process.groupAlive()
		if probeErr != nil || !alive || attempt == 40 {
			uncertain = uncertain || alive || probeErr != nil
			break
		}
		settle := execution.clock.timer(25 * time.Millisecond)
		<-settle.channel()
		settle.stop()
	}
	// 最终回收之后禁止任何组探测或信号操作。
	exit, waitErr := process.wait()
	waited := processWaitResult{exit: exit, err: waitErr}
	sandboxFailed := errors.Is(waitErr, errSandboxUnavailable)
	uncertain = uncertain || (waitErr != nil && !sandboxFailed)
	// 输出与进程退出分别收口，脱离writer不能无限阻塞reader。
	drain := execution.clock.timer(time.Second)
	var outResult, errResult processReadResult
	outChannel, errChannel := (<-chan processReadResult)(outDone), (<-chan processReadResult)(errDone)
	drainExpired := false
	for outChannel != nil || errChannel != nil {
		select {
		case outResult = <-outChannel:
			outChannel = nil
		case errResult = <-errChannel:
			errChannel = nil
		case <-drain.channel():
			drainExpired = true
			_ = stdout.Close()
			_ = stderr.Close()
			if outChannel != nil {
				outResult = <-outChannel
				outChannel = nil
			}
			if errChannel != nil {
				errResult = <-errChannel
				errChannel = nil
			}
		}
	}
	drain.stop()
	close(outDone)
	close(errDone)
	_ = stdout.Close()
	_ = stderr.Close()
	execution.progress.update(nil, false, max(0, execution.clock.now().Sub(started).Milliseconds()))
	uncertain = uncertain || drainExpired || outResult.err != nil || errResult.err != nil
	if sandboxFailed && !uncertain {
		// 基础设施错误不冒充用户命令退出，也不回传可能含宿主路径的sandbox诊断。
		if cancelled {
			return failed(tool.ResultCancelled, "cancelled")
		}
		if timedOut {
			return failed(tool.ResultError, "timed_out")
		}
		return failed(tool.ResultError, "sandbox_unavailable")
	}
	status, code := tool.ResultSuccess, "ok"
	if cancelled {
		status, code = tool.ResultCancelled, "cancelled"
	}
	if timedOut {
		status, code = tool.ResultError, "timed_out"
	}
	if uncertain {
		status, code = tool.ResultOutcomeUncertain, "process_cleanup_uncertain"
	}
	if drainExpired {
		code = "output_cleanup_incomplete"
	}
	metadata, err := tool.NewExecResultMetadata(execution.candidate.Invocation().Input().Workdir(), waited.exit, waited.err == nil, max(0, execution.clock.now().Sub(started).Milliseconds()), outResult.output.Stats(), errResult.output.Stats(), timedOut, cancelled, false)
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	truncated := tool.ExecOutputNeedsSaving(outResult.output, errResult.output, execution.candidate.PreviewAllowance())
	needsSaving := truncated || drainExpired || outResult.err != nil || errResult.err != nil
	var preview tool.ModelPreview
	if capture != nil {
		outSaving, errSaving := capture.Finish(outResult.err == nil && !drainExpired, errResult.err == nil && !drainExpired, needsSaving)
		metadata, err = metadata.WithOutputSaving(outSaving, errSaving)
	} else if needsSaving && captureErr != nil {
		reason := "storage_unavailable"
		if errors.Is(captureErr, tool.ArtifactQuotaExceeded) {
			reason = "quota_exceeded"
		}
		saving, _ := tool.NewArtifactSaving(tool.ArtifactUnavailable, reason, tool.ArtifactDescriptor{})
		metadata, err = metadata.WithOutputSaving(saving, saving)
	}
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	preview, truncated, err = execution.renderer(outResult.output, errResult.output, metadata, execution.candidate.PreviewAllowance())
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	preview, err = tool.NewModelPreview("Exec outcome: " + string(status) + " (" + code + ")\n" + preview.Text())
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	updated, updateErr := tool.NewExecResultMetadata(metadata.Workdir(), waited.exit, waited.err == nil, metadata.DurationMillis(), metadata.Stdout(), metadata.Stderr(), timedOut, cancelled, truncated)
	if updateErr == nil {
		metadata, err = updated.WithOutputSaving(metadata.StdoutSaving(), metadata.StderrSaving())
	} else {
		err = updateErr
	}
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	result, err := tool.NewExecInvocationResult(execution.candidate.Invocation(), status, code, preview, metadata)
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	reason := tool.BudgetPreserved
	if truncated {
		reason = tool.BudgetShortened
	}
	budget, budgetErr := tool.NewResultBudgetMetadata(execution.candidate.PreviewAllowance(), len(preview.Text()), reason)
	if budgetErr != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	result, err = result.WithBudget(budget)
	if err != nil {
		return failed(tool.ResultOutcomeUncertain, "invalid_process_result")
	}
	return result
}
