package protocol

import (
	"context"
	"errors"

	"easycode/internal/domain"
)

// CommandStream 标识宿主查询的命令输出流。
type CommandStream string

const (
	CommandStdout CommandStream = "stdout"
	CommandStderr CommandStream = "stderr"
)

// CommandOutputSummary 分别描述保存完整性与预览省略，不暴露私有路径。
type CommandOutputSummary struct {
	Status        string
	Reason        string
	SavedBytes    int64
	ObservedBytes int64
	Complete      bool
	Readable      bool
}

// CommandHistoryItem 是已提交命令事实的宿主投影。
type CommandHistoryItem struct {
	Sequence       uint64
	InvocationID   string
	Command        string
	Workdir        string
	Status         string
	Code           string
	Preview        string
	Terminal       bool
	ExitCode       int
	ExitKnown      bool
	DurationMillis int64
	Truncated      bool
	Stdout         CommandOutputSummary
	Stderr         CommandOutputSummary
}

// CommandListQuery 使用严格递减的 ready seq 游标；零游标表示最新页。
type CommandListQuery struct {
	ThreadID domain.ThreadID
	Before   uint64
	Limit    uint32
}

func (q CommandListQuery) Validate() error {
	if !q.ThreadID.Valid() || q.Limit == 0 || q.Limit > 50 {
		return errors.New("invalid command list query")
	}
	return nil
}

// CommandListPage 按 ready seq 降序返回最多五十条记录。
type CommandListPage struct {
	Items      []CommandHistoryItem
	NextBefore uint64
}

// CommandOutputQuery 只使用当前线程的命令身份，不接受任意路径或 artifact ID。
type CommandOutputQuery struct {
	ThreadID     domain.ThreadID
	InvocationID string
	Stream       CommandStream
	Offset       uint64
	Limit        uint64
}

func (q CommandOutputQuery) Validate() error {
	if !q.ThreadID.Valid() || !validUUIDv7Text(q.InvocationID) || (q.Stream != CommandStdout && q.Stream != CommandStderr) || q.Limit == 0 || q.Limit > 65536 {
		return errors.New("invalid command output query")
	}
	return nil
}

// CommandOutputPage 的偏移以原始 bytes 计量，Text 为已校验范围的展示副本。
type CommandOutputPage struct {
	Offset         uint64
	NextOffset     uint64
	StoredBytes    uint64
	StoredEOF      bool
	SourceComplete bool
	Text           string
}

// CommandQueries 是宿主提供给界面的只读查询端口，不产生模型请求或工具调用。
type CommandQueries interface {
	ListCommands(context.Context, CommandListQuery) (CommandListPage, error)
	ReadCommandOutput(context.Context, CommandOutputQuery) (CommandOutputPage, error)
}
