package builtin

import (
	"context"
	"errors"
	"time"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
)

// HookOutputParser 是纯内存输出策略；受控字节不传播到执行结果。
type HookOutputParser func(domain.HookEvent, domain.HookFeedbackPolicy, int, []byte) domain.HookParsedOutput
type hookProcessLauncher interface {
	startHook(context.Context, domain.HookDeclaration) (process.Handle, error)
	preflight(context.Context) domain.HookCode
}

// HookRunner 复用进程 owner，但不借用工具 invocation、批准或输出保存。
type HookRunner struct {
	launcher hookProcessLauncher
	clock    process.Clock
	parse    HookOutputParser
}

func NewHookRunner(workspace *Workspace, environment CommandEnvironment, parse HookOutputParser) (*HookRunner, error) {
	if workspace == nil || parse == nil {
		return nil, domain.HookConfigInvalid
	}
	environment.entries = append([]string(nil), environment.entries...)
	return &HookRunner{launcher: newHookProcessLauncher(workspace, environment), clock: process.WallClock{}, parse: parse}, nil
}
func (r *HookRunner) Preflight(ctx context.Context) domain.HookCode { return r.launcher.preflight(ctx) }

type hookProcessStart struct {
	launcher    hookProcessLauncher
	declaration domain.HookDeclaration
}

func (l hookProcessStart) Start(ctx context.Context) (process.Handle, error) {
	return l.launcher.startHook(ctx, l.declaration)
}

type hookOutputBuffer struct {
	data     []byte
	observed int64
}

func (b *hookOutputBuffer) Write(p []byte) (int, error) {
	b.observed += int64(len(p))
	remaining := domain.MaxHookOutputBytes - len(b.data)
	b.data = append(b.data, p[:min(len(p), remaining)]...)
	if len(p) > remaining {
		return 0, domain.HookOutputLimit
	}
	return len(p), nil
}

// Run 在已 durable 的 Hook started 后接收一次检查；返回前完成全部 owned 清理。
func (r *HookRunner) Run(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration, input domain.HookInput, timeout time.Duration) domain.HookExecution {
	result := domain.HookRunResult{Identity: id, Status: domain.HookError, Code: domain.HookStartFailed, CleanupConfirmed: true, Origin: "execution"}
	if id.Validate() != nil || d.Validate() != nil || input.Validate() != nil || input.Event != id.Event || d.Reference.Event != id.Event || timeout <= 0 || timeout > time.Duration(d.Reference.TimeoutSeconds)*time.Second {
		return domain.HookExecution{Result: result}
	}
	if ctx == nil || ctx.Err() != nil {
		result.Status = domain.HookCancel
		result.Code = domain.HookCancelled
		result.Cancelled = true
		return domain.HookExecution{Result: result}
	}
	b, err := input.Bytes()
	if err != nil {
		result.Code = domain.HookInputLimit
		return domain.HookExecution{Result: result}
	}
	out, stderr := &hookOutputBuffer{}, &hookOutputBuffer{}
	owner, err := process.New(process.Config{Launcher: hookProcessStart{r.launcher, d}, Clock: r.clock, Input: b, Stdout: out, Stderr: stderr, Timeout: timeout})
	if err != nil {
		return domain.HookExecution{Result: result}
	}
	owned := owner.Run(ctx)
	owned.Cancelled = owned.Cancelled || ctx.Err() != nil
	return classifyHookExecution(id, d.Reference.Feedback, owned, out.data, out.observed, stderr.observed, r.parse)
}

// classifyHookExecution 固定故障优先级，只有可确认的正常退出才解析 stdout。
func classifyHookExecution(id domain.HookRunIdentity, policy domain.HookFeedbackPolicy, owned process.Result, stdout []byte, stdoutBytes, stderrBytes int64, parse HookOutputParser) domain.HookExecution {
	result := domain.HookRunResult{Identity: id, Status: domain.HookError, Code: domain.HookStartFailed, CleanupConfirmed: true, Origin: "execution"}
	var feedback domain.HookFeedbackCandidate
	result.ExitKnown = owned.ExitKnown
	if owned.ExitKnown {
		result.ExitCode = owned.ExitCode
	}
	result.Cancelled = owned.Cancelled
	result.TimedOut = owned.TimedOut
	result.StdoutBytes, result.StderrBytes = stdoutBytes, stderrBytes
	switch {
	case owned.Uncertain || owned.AlreadyAccepted:
		result.Status, result.Code, result.CleanupConfirmed = domain.HookUncertain, domain.HookOutcomeUncertain, false
	case result.Cancelled:
		result.Status, result.Code = domain.HookCancel, domain.HookCancelled
	case owned.TimedOut:
		result.Code = domain.HookTimedOut
	case stdoutBytes > domain.MaxHookOutputBytes || stderrBytes > domain.MaxHookOutputBytes:
		result.Code = domain.HookOutputLimit
	case owned.StdinError != nil || owned.StdoutError != nil || owned.StderrError != nil:
		result.Code = domain.HookIOFailed
	case errors.Is(owned.StartError, process.Unsupported):
		result.Code = domain.HookUnsupported
	case errors.Is(owned.StartError, process.SandboxUnavailable) || errors.Is(owned.WaitError, process.SandboxUnavailable):
		result.Code = domain.HookSandboxUnavailable
	case owned.StartError != nil:
		result.Code = domain.HookStartFailed
	default:
		parsed := parse(id.Event, policy, owned.ExitCode, stdout)
		result.Status, result.Code = parsed.Status, parsed.Code
		feedback = parsed.Feedback
	}
	execution, validationErr := domain.NewHookExecution(result, policy, feedback)
	if validationErr != nil {
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}}
	}
	return execution
}
