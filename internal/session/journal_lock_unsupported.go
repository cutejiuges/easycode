//go:build !darwin && !linux && !windows

package session

import (
	"fmt"
	"os"
)

func tryLockJournal(*os.File) error {
	return fmt.Errorf("session journal locking is unsupported on this platform")
}
