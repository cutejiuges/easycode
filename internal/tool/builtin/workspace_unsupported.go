//go:build !darwin && !linux

package builtin

import "os"

type unsupportedWorkspaceHandle struct{}
type unsupportedWorkspaceDirectory struct{}
type unsupportedWorkspaceChild struct{}

func openWorkspaceHandle(string) (workspaceHandle, error) { return nil, errWorkspaceUnsupported }
func (unsupportedWorkspaceHandle) openFile(string, workspaceHooks) (*os.File, error) {
	return nil, errWorkspaceUnsupported
}
func (unsupportedWorkspaceHandle) openDirectory(string, workspaceHooks) (workspaceDirectory, error) {
	return nil, errWorkspaceUnsupported
}
func (unsupportedWorkspaceHandle) close() error { return nil }
func (unsupportedWorkspaceDirectory) readEntryNames() ([]string, error) {
	return nil, errWorkspaceUnsupported
}
func (unsupportedWorkspaceDirectory) openChild(string, workspaceHooks) (workspaceChild, error) {
	return nil, errWorkspaceUnsupported
}
func (unsupportedWorkspaceDirectory) close() error              { return nil }
func (unsupportedWorkspaceChild) kind() workspaceNodeKind       { return workspaceNodeUnsupported }
func (unsupportedWorkspaceChild) size() int64                   { return 0 }
func (unsupportedWorkspaceChild) file() *os.File                { return nil }
func (unsupportedWorkspaceChild) directory() workspaceDirectory { return nil }
func (unsupportedWorkspaceChild) close() error                  { return nil }
