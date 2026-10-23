//go:build darwin || linux

package builtin

import (
	"golang.org/x/sys/unix"
	"os"
)

func readFileSnapshot(file *os.File) (readFileState, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return readFileState{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return readFileState{}, errTargetNotRegular
	}
	return readFileState{device: uint64(stat.Dev), inode: uint64(stat.Ino), mode: uint32(stat.Mode & 0o7777), size: stat.Size, modified: [4]int64{stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec}}, nil
}
