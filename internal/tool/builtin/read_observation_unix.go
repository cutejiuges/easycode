//go:build darwin || linux

package builtin

import (
	"golang.org/x/sys/unix"
	"os"
)

func readFileSnapshot(file *os.File) (readFileState, error) {
	connection, err := file.SyscallConn()
	if err != nil {
		return readFileState{}, err
	}
	var stat unix.Stat_t
	var statErr error
	// 读取取消可能并发关闭文件，fstat 必须和实际描述符的借用绑定。
	if err := connection.Control(func(fd uintptr) { statErr = unix.Fstat(int(fd), &stat) }); err != nil {
		return readFileState{}, err
	}
	if statErr != nil {
		return readFileState{}, statErr
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return readFileState{}, errTargetNotRegular
	}
	return readFileState{device: uint64(stat.Dev), inode: uint64(stat.Ino), mode: uint32(stat.Mode & 0o7777), size: stat.Size, modified: [4]int64{stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec}}, nil
}
