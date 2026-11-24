package session

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameBranchNoReplace(directory *os.File, from, to string) error {
	return unix.Renameat2(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_NOREPLACE)
}
