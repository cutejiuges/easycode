//go:build !darwin

package builtin

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
)

type unsupportedHookLauncher struct{}

func newHookProcessLauncher(*Workspace, CommandEnvironment) hookProcessLauncher {
	return unsupportedHookLauncher{}
}
func (unsupportedHookLauncher) preflight(context.Context) domain.HookCode {
	return domain.HookUnsupported
}
func (unsupportedHookLauncher) startHook(context.Context, domain.HookDeclaration) (process.Handle, error) {
	return nil, process.Unsupported
}
