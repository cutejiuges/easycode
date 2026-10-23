package builtin

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"easycode/internal/tool"
)

var errExecUnsupported = errors.New("unsupported_platform")
var errSandboxUnavailable = errors.New("sandbox_unavailable")

// CommandEnvironment 保存显式捕获的最小环境，实际值不进入工具契约。
type CommandEnvironment struct {
	entries    []string
	executable string
}

// OpenCommandEnvironment 在应用生命周期中显式读取宿主环境。
func OpenCommandEnvironment() CommandEnvironment {
	environment := filterCommandEnvironment(os.Environ())
	environment.executable, _ = os.Executable()
	return environment
}

func filterCommandEnvironment(source []string) CommandEnvironment {
	values := make(map[string]string)
	for _, entry := range source {
		name, value, found := strings.Cut(entry, "=")
		if !found || strings.ContainsRune(value, 0) {
			continue
		}
		switch name {
		case "PATH", "LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_NUMERIC":
			values[name] = value
		}
	}
	if values["PATH"] == "" {
		values["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	values["NO_COLOR"], values["TERM"], values["PAGER"], values["GIT_PAGER"] = "1", "dumb", "cat", "cat"
	values["GIT_TERMINAL_PROMPT"] = "0"
	entries := make([]string, 0, len(values))
	for key, value := range values {
		entries = append(entries, key+"="+value)
	}
	sort.Strings(entries)
	return CommandEnvironment{entries: entries}
}

// ExecPlanner 在内存中冻结命令候选，不进行目录或进程操作。
type ExecPlanner struct {
	shell       tool.ShellProfile
	environment tool.EnvironmentPolicy
	sandbox     tool.SandboxProfile
}

// NewExecPlanner 创建首版固定前台命令策略。
func NewExecPlanner() *ExecPlanner {
	shell, _ := tool.NewShellProfile("posix-sh-nonlogin", "/bin/sh")
	environment, _ := tool.NewEnvironmentPolicy("minimal-command-environment", []string{"PATH", "LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_NUMERIC"})
	sandbox, _ := tool.NewSandboxProfile("darwin-workspace-offline", false, true)
	return &ExecPlanner{shell: shell, environment: environment, sandbox: sandbox}
}

// Prepare 仅组合已验证输入与固定profile。
func (planner *ExecPlanner) Prepare(invocation tool.ExecInvocation) (tool.ExecCandidate, error) {
	return tool.NewExecCandidate(invocation, planner.shell, planner.environment, planner.sandbox)
}

// ExecExecutor 组合工作区、环境快照与平台启动边界。
type ExecExecutor struct {
	launcher processLauncher
	renderer tool.ExecOutputRenderer
}

// NewExecExecutor 纯内存构造命令执行器。
func NewExecExecutor(workspace *Workspace, environment CommandEnvironment, renderer tool.ExecOutputRenderer) (*ExecExecutor, error) {
	if workspace == nil || workspace.handle == nil || len(environment.entries) == 0 || renderer == nil {
		return nil, errors.New("command workspace and environment are required")
	}
	return &ExecExecutor{launcher: newPlatformProcessLauncher(workspace, CommandEnvironment{entries: append([]string(nil), environment.entries...), executable: environment.executable}), renderer: renderer}, nil
}

// Execute 持有唯一执行owner直到最终清理；progress回调只在宿主路径调用。
func (executor *ExecExecutor) Execute(ctx context.Context, candidate tool.ExecCandidate, progress func(tool.ExecProgress)) tool.InvocationResult {
	execution, err := newProcessExecution(executor.launcher, wallProcessClock{}, candidate, executor.renderer)
	if err != nil {
		return tool.NewExecErrorResult(candidate.Invocation(), tool.ResultError, "invalid_candidate", "Exec failed: invalid_candidate")
	}
	result := make(chan tool.InvocationResult, 1)
	go func() { result <- execution.Run(ctx); close(result) }()
	for range execution.Notifications() {
		if progress != nil {
			progress(execution.Progress())
		}
	}
	value := <-result
	<-execution.Done()
	return value
}
