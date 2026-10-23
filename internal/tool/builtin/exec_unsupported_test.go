//go:build !darwin

package builtin

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/tool"
)

func TestUnsupportedCommandAdapterFailsClosed(t *testing.T) {
	launcher := newPlatformProcessLauncher(nil, CommandEnvironment{})
	handle, err := launcher.start(context.Background(), tool.ExecCandidate{})
	if handle != nil || !errors.Is(err, errExecUnsupported) {
		t.Fatal("unsupported adapter did not fail closed")
	}
	handled, err := RunCommandChild([]string{"--internal-command-child"})
	if !handled || !errors.Is(err, errExecUnsupported) {
		t.Fatal("unsupported child helper accepted execution")
	}
}
