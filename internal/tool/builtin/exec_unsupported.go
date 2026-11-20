//go:build !darwin

package builtin

import "easycode/internal/executor/process"

func newPlatformProcessLauncher(*Workspace, CommandEnvironment) processLauncher {
	return unsupportedProcessLauncher{}
}

// RunCommandChild 在没有安全adapter的平台拒绝内部启动入口。
func RunCommandChild(arguments []string) (bool, error) {
	if len(arguments) > 0 && arguments[0] == "--internal-command-child" {
		return true, process.Unsupported
	}
	return false, nil
}
