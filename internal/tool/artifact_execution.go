package tool

import "context"

// CommandOutputCapture 是一次命令输出保存的唯一资源 owner。
// 两个流各有一个 writer；Finish 与 Close 只能在两个 writer 都完成后调用。
type CommandOutputCapture interface {
	WriteStdout([]byte) (int, error)
	WriteStderr([]byte) (int, error)
	Finish(stdoutEOF, stderrEOF, needed bool) (ArtifactSaving, ArtifactSaving)
	Close() error
}

// CommandOutputStore 只负责接收已经进入 started 的命令保存请求。
type CommandOutputStore interface {
	StartCapture(context.Context, InvocationID) (CommandOutputCapture, error)
}

// ArtifactRangeReader 只接收当前线程已授权引用的范围请求。
type ArtifactRangeReader interface {
	ReadArtifact(context.Context, ReadArtifactInput) (ArtifactWindow, error)
}

// TextOutputCapture 是一个搜索调用的文本保存 owner；调用者在写入结束后依次 Finish、Close。
// WriteText 只接收完整 UTF-8 文本，保存失败不阻止搜索继续统计。
type TextOutputCapture interface {
	WriteText(string) error
	Finish(complete, needed bool) ArtifactSaving
	Close() error
}

// TextOutputStore 在 started 持久化后按调用顺序获取搜索保存资源。
type TextOutputStore interface {
	StartTextCapture(context.Context, InvocationID, CapabilityID) (TextOutputCapture, error)
}
