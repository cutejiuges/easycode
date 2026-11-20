package builtin

import (
	"context"
	"os"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
)

type darwinHookLauncher struct{ launcher *darwinProcessLauncher }

func newHookProcessLauncher(workspace *Workspace, environment CommandEnvironment) hookProcessLauncher {
	return darwinHookLauncher{&darwinProcessLauncher{workspace: workspace, environment: environment}}
}
func (l darwinHookLauncher) preflight(ctx context.Context) domain.HookCode {
	if ctx.Err() != nil {
		return domain.HookCancelled
	}
	if l.launcher.environment.executable == "" {
		return domain.HookSandboxUnavailable
	}
	f, err := os.Open("/usr/bin/sandbox-exec")
	if err != nil {
		return domain.HookSandboxUnavailable
	}
	defer f.Close()
	s, err := f.Stat()
	if err != nil || !s.Mode().IsRegular() || s.Mode().Perm()&0111 == 0 {
		return domain.HookSandboxUnavailable
	}
	return domain.HookOK
}
func (l darwinHookLauncher) startHook(ctx context.Context, d domain.HookDeclaration) (process.Handle, error) {
	if d.Validate() != nil {
		return nil, process.SandboxUnavailable
	}
	return l.launcher.startSpec(ctx, commandLaunchSpec{
		command: d.Command, pipeInput: true, policy: hookLaunchPolicy{d.Reference.Fingerprint},
	})
}
