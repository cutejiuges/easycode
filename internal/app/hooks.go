package app

import (
	"context"
	"time"

	"easycode/internal/domain"
	"easycode/internal/extension/hook"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session/artifact"
	"easycode/internal/tool/builtin"
)

// hookArtifactBoundary 复用私有 Session 存储隔离检查，不让 Hook 读取存储布局。
type hookArtifactBoundary struct {
	layout *artifact.Layout
	runner chatRuntime.HookRunner
}

func (b hookArtifactBoundary) Preflight(ctx context.Context) domain.HookCode {
	if ctx.Err() != nil {
		return domain.HookCancelled
	}
	if b.layout.Check(ctx) != nil {
		return domain.HookSandboxUnavailable
	}
	return b.runner.Preflight(ctx)
}
func (b hookArtifactBoundary) Run(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration, input domain.HookInput, timeout time.Duration) domain.HookExecution {
	if b.layout.Check(ctx) != nil {
		if ctx.Err() != nil {
			return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true, Origin: "execution"}}
		}
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookError, Code: domain.HookSandboxUnavailable, CleanupConfirmed: true, Origin: "execution"}}
	}
	return b.runner.Run(ctx, id, d, input, timeout)
}
func loadChatHookSnapshot(ctx context.Context, cwd, home string, hooks chatResourceHooks) (domain.HookSnapshot, error) {
	load := hooks.loadHooks
	if load == nil {
		load = hook.NewLoader().Load
	}
	return load(ctx, cwd, home)
}

func installChatHooks(ctx context.Context, r *chatRuntime.Runtime, workspace *builtin.Workspace, layout *artifact.Layout, cwd string, snapshot domain.HookSnapshot, hooks chatResourceHooks) error {
	var runner chatRuntime.HookRunner
	var err error
	if hooks.newHookRunner != nil {
		runner, err = hooks.newHookRunner(workspace, builtin.OpenCommandEnvironment(), hook.ParseExecution)
	} else {
		runner, err = builtin.NewHookRunner(workspace, builtin.OpenCommandEnvironment(), hook.ParseExecution)
	}
	if err != nil {
		return err
	}
	boundary := hookArtifactBoundary{layout, runner}
	availability := boundary.Preflight(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return r.InstallHooks(snapshot, boundary, cwd, availability)
}
