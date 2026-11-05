package builtin

import (
	"context"
	"easycode/internal/executor/process"
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

	outWriter := &processOutputWriter{progress: execution.progress, clock: execution.clock, started: started}
	errWriter := &processOutputWriter{progress: execution.progress, stderr: true, clock: execution.clock, started: started}
	if capture != nil {
		outWriter.save = capture.WriteStdout
		errWriter.save = capture.WriteStderr
	}
	owner, err := process.New(process.Config{Launcher: execOwnerLauncher{execution.launcher, execution.candidate}, Clock: ownerClock{execution.clock}, Stdout: outWriter, Stderr: errWriter, Timeout: time.Duration(execution.candidate.Invocation().Input().TimeoutMillis()) * time.Millisecond})
	if err != nil {
		return failed(tool.ResultError, "process_start_failed")
	}
	owned := owner.Run(ctx)
	if owned.StartError != nil {
		if errors.Is(owned.StartError, process.Unsupported) {
			return failed(tool.ResultError, "unsupported_platform")
		}
		if errors.Is(owned.StartError, process.SandboxUnavailable) {
			return failed(tool.ResultError, "sandbox_unavailable")
		}
		return failed(tool.ResultError, "process_start_failed")
	}
	if !owned.Started {
		return failed(tool.ResultCancelled, "cancelled")
	}
	cancelled, timedOut, uncertain := owned.Cancelled, owned.TimedOut, owned.Uncertain
	drainExpired := owned.DrainExpired
	waited := processWaitResult{exit: owned.ExitCode, err: owned.WaitError}
	sandboxFailed := errors.Is(owned.WaitError, process.SandboxUnavailable)
	outResult := processReadResult{output: outWriter.buffer.Snapshot(), err: owned.StdoutError}
	errResult := processReadResult{output: errWriter.buffer.Snapshot(), err: owned.StderrError}

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
