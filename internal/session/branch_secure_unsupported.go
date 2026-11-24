//go:build !darwin && !linux

package session

import (
	"errors"
	"os"
)

var errUnsupportedBranchPublication = errors.New("secure branch journal publication is unsupported on this platform")

func (*secureRoot) hooksForBranch() securePathHooks    { return securePathHooks{} }
func (*secureRoot) syncBranchFile(*os.File) error      { return errUnsupportedBranchPublication }
func (*secureRoot) syncBranchDirectory(*os.File) error { return errUnsupportedBranchPublication }
func (*secureRoot) checkBranchRoute(string, *os.File, string, *os.File) error {
	return errUnsupportedBranchPublication
}
func checkBranchJournalEntry(*os.File, string, *os.File) error {
	return errUnsupportedBranchPublication
}
func unlinkBranchStage(*os.File, string) error             { return errUnsupportedBranchPublication }
func renameBranchNoReplace(*os.File, string, string) error { return errUnsupportedBranchPublication }
