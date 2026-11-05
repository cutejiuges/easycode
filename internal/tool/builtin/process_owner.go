package builtin

import (
	"context"
	"errors"
	"io"
	"time"

	"easycode/internal/executor/process"
	"easycode/internal/tool"
)

// execOwnerLauncher 将当前候选绑定到共用生命周期，不改变普通工具准入。
type execOwnerLauncher struct {
	launcher  processLauncher
	candidate tool.ExecCandidate
}

func (l execOwnerLauncher) Start(ctx context.Context) (process.Handle, error) {
	h, err := l.launcher.start(ctx, l.candidate)
	if err != nil {
		return nil, ownerProcessError(err)
	}
	return ownerHandle{handle: h}, nil
}

type ownerHandle struct {
	handle processHandle
	input  io.WriteCloser
}

func (h ownerHandle) Stdin() io.WriteCloser { return h.input }
func (h ownerHandle) Stdout() io.ReadCloser { return h.handle.stdout() }
func (h ownerHandle) Stderr() io.ReadCloser { return h.handle.stderr() }
func (h ownerHandle) ObserveExit() error    { return h.handle.observeExit() }
func (h ownerHandle) Wait() (int, error) {
	exit, err := h.handle.wait()
	return exit, ownerProcessError(err)
}
func (h ownerHandle) Terminate(force bool) error { return h.handle.terminate(force) }
func (h ownerHandle) GroupAlive() (bool, error)  { return h.handle.groupAlive() }
func ownerProcessError(err error) error {
	if errors.Is(err, errExecUnsupported) {
		return process.Unsupported
	}
	if errors.Is(err, errSandboxUnavailable) {
		return process.SandboxUnavailable
	}
	return err
}

type ownerClock struct{ clock processClock }

func (c ownerClock) Now() time.Time                      { return c.clock.now() }
func (c ownerClock) Timer(d time.Duration) process.Timer { return ownerTimer{c.clock.timer(d)} }

type ownerTimer struct{ timer processTimer }

func (t ownerTimer) Channel() <-chan time.Time { return t.timer.channel() }
func (t ownerTimer) Stop()                     { t.timer.stop() }
