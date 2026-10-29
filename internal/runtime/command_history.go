package runtime

import "easycode/internal/tool"

// CommandHistoryObserver 接收已成功 Sync 的命令事实，消费者仅更新内存投影。
type CommandHistoryObserver interface {
	CommandReady(uint64, tool.Invocation)
	CommandResult(tool.InvocationResult)
}
