//go:build !darwin

package builtin

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
)

func TestHookUnsupportedPlatformNeverLaunches(t *testing.T) {
	launcher := newHookProcessLauncher(nil, CommandEnvironment{})
	if launcher.preflight(context.Background()) != domain.HookUnsupported {
		t.Fatal("unsupported platform advertised execution")
	}
	handle, err := launcher.startHook(context.Background(), domain.HookDeclaration{})
	if handle != nil || !errors.Is(err, process.Unsupported) {
		t.Fatal("unsandboxed fallback available", err)
	}
}
