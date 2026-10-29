//go:build linux

package artifact

import (
	"golang.org/x/sys/unix"
	"os"
)

func publishObject(directory *os.File, from, to string) error {
	return unix.Renameat2(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_NOREPLACE)
}
