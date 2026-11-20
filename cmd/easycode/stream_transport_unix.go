//go:build darwin || linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// openCLIStreamFile 把继承的描述符交给 Go poller，使 Close 能解除阻塞读写。
func openCLIStreamFile(original *os.File) (*os.File, func(), error) {
	raw, err := original.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var fd int
	var changed bool
	if controlErr := raw.Control(func(originalFD uintptr) {
		var flags int
		flags, err = unix.FcntlInt(originalFD, unix.F_GETFL, 0)
		if err != nil {
			return
		}
		fd, err = unix.FcntlInt(originalFD, unix.F_DUPFD_CLOEXEC, 3)
		if err != nil {
			return
		}
		if flags&unix.O_NONBLOCK == 0 {
			err = unix.SetNonblock(fd, true)
			if err != nil {
				_ = unix.Close(fd)
				return
			}
			changed = true
		}
	}); controlErr != nil {
		return nil, nil, controlErr
	}
	if err != nil {
		return nil, nil, err
	}

	stream := os.NewFile(uintptr(fd), original.Name())
	release := func() {
		_ = stream.Close()
		// 复制的描述符与原描述符共享状态标志。结束后恢复原阻塞行为。
		if changed {
			_ = raw.Control(func(originalFD uintptr) { _ = unix.SetNonblock(int(originalFD), false) })
		}
	}
	return stream, release, nil
}
