package runtime

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// ToolHistoryObserver 接收已成功 Sync 且已 finalize 的事实，消费者仅更新内存投影。
type ToolHistoryObserver interface {
	NativeCommitted(uint64, []session.ToolHistoryEntry) error
	ToolResult(tool.InvocationResult)
}

func (runtime *Runtime) emitTranscriptCommit(turnID domain.TurnID, sequence uint64) {
	if runtime.toolHistory == nil || runtime.turnEmitter == nil {
		return
	}
	event, err := protocol.NewTranscriptCommitted(sequence)
	if err == nil {
		runtime.turnEmitter(runtime.decorate(event, turnID))
	}
}

// CompactionHistoryObserver 只在成功 Sync 和安装之后更新内存摘要边界。
type CompactionHistoryObserver interface {
	CompactionCommitted(uint64, domain.CompactionID, domain.ContextSummary) error
}
