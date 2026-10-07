//go:build !darwin && !linux

package builtin

import (
	"os"
)

type workspaceHandle interface {
	openFile(string, workspaceHooks) (*os.File, error)
	close() error
}

type unsupportedWorkspaceHandle struct{}

func openWorkspaceHandle(string) (workspaceHandle, error) {
	return nil, errWorkspaceUnsupported
}

func (unsupportedWorkspaceHandle) openFile(string, workspaceHooks) (*os.File, error) {
	return nil, errWorkspaceUnsupported
}

func (unsupportedWorkspaceHandle) close() error { return nil }
