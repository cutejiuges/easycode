//go:build darwin || linux

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockJournal(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errJournalBusy
	}
	return err
}
