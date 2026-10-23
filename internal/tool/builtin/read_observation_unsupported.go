//go:build !darwin && !linux

package builtin

import "os"

func readFileSnapshot(*os.File) (readFileState, error) {
	return readFileState{}, errWorkspaceUnsupported
}
