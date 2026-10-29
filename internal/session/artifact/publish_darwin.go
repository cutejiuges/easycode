//go:build darwin

package artifact

import (
	"golang.org/x/sys/unix"
	"os"
)

func publishObject(directory *os.File, from, to string) error {
	return unix.RenameatxNp(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_EXCL)
}
