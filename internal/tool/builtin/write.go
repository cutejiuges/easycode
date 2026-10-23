package builtin

import (
	"context"
	"errors"
	"os"
	"slices"
	"unicode/utf8"

	"easycode/internal/tool"
)

// WritePreparationError 只携带稳定错误码，底层路径和文件正文不进入错误。
type WritePreparationError struct {
	code   string
	status tool.ResultStatus
}

func (err *WritePreparationError) Error() string             { return "Write preparation failed: " + err.code }
func (err *WritePreparationError) Code() string              { return err.code }
func (err *WritePreparationError) Status() tool.ResultStatus { return err.status }
func writeError(code string) error {
	return &WritePreparationError{code: code, status: tool.ResultError}
}
func writeCancelledError() error {
	return &WritePreparationError{code: "cancelled", status: tool.ResultCancelled}
}
func writeConflictError() error {
	return &WritePreparationError{code: "write_conflict", status: tool.ResultConflict}
}

var (
	errWriteConflict    = errors.New("write candidate conflicts with workspace")
	errWriteUnsupported = errors.New("atomic Write publication is unsupported")
	errWriteUncertain   = errors.New("write publication or cleanup is uncertain")
)

type writeInspection struct {
	operation   tool.WriteOperation
	base        tool.FileBaseEvidence
	ancestors   []tool.DirectoryEvidence
	directories []string
	content     []byte
}

type writePath interface {
	inspect(context.Context) (writeInspection, error)
	publish(context.Context, tool.WriteCandidate, writeHooks) error
	close() error
}

// writeHooks 仅在显式执行边界注入故障，不创建隐式后台任务。
type writeHooks struct {
	write         func(*os.File, []byte) error
	chmod         func(*os.File, os.FileMode) error
	fileSync      func(*os.File) error
	directorySync func(*os.File) error
	beforePublish func()
	afterPublish  func()
	publish       func(int, string, string, bool) error
	beforeCleanup func()
	unlink        func(int, string, bool) error
	close         func(*os.File) error
}

// WritePreparer 持有显式打开的工作区，只读生成不可变候选。
type WritePreparer struct{ workspace *Workspace }

// NewWritePreparer 纯内存装配，不探测或创建文件。
func NewWritePreparer(workspace *Workspace) (*WritePreparer, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &WritePreparer{workspace: workspace}, nil
}

// Prepare 区分创建与覆盖，只有来自先前完整输出的观察能授权覆盖。
func (p *WritePreparer) Prepare(ctx context.Context, v tool.WriteInvocation, observation tool.ReadObservation) (candidate tool.WriteCandidate, resultErr error) {
	if ctx == nil || ctx.Err() != nil {
		return candidate, writeCancelledError()
	}
	if p == nil || p.workspace == nil || v.Validate() != nil || observation.Validate() != nil {
		return candidate, writeError("invalid_invocation")
	}
	path, err := p.workspace.handle.openWritePath(v.Input().FilePath(), p.workspace.hooks)
	if err != nil {
		return candidate, safeWriteOpenError(err)
	}
	defer func() {
		if path.close() != nil {
			candidate = tool.WriteCandidate{}
			resultErr = writeError("write_target_unavailable")
		}
	}()
	state, err := path.inspect(ctx)
	if err != nil {
		return candidate, safeWriteOpenError(err)
	}
	if state.operation == tool.WriteOverwrite {
		if observation.Kind() != tool.ReadObservationComplete || observation.RelativePath() != v.Input().FilePath() {
			return candidate, writeError("write_requires_full_read")
		}
		if observation.Evidence() != state.base {
			return candidate, writeConflictError()
		}
		if !utf8.Valid(state.content) || containsNUL(state.content) {
			return candidate, writeError("invalid_text_file")
		}
		if string(state.content) == v.Input().Content() {
			return candidate, writeError("write_no_changes")
		}
	}
	preview, err := buildWritePreview(v.Input(), state.operation, state.content, state.directories)
	if err != nil {
		return candidate, err
	}
	if ctx.Err() != nil {
		return candidate, writeCancelledError()
	}
	return tool.NewWriteCandidate(v, state.operation, state.base, state.ancestors, state.directories, preview)
}

// WriteExecutor 独占执行经过批准的候选，持有所有暂存资源到清理结束。
type WriteExecutor struct {
	workspace *Workspace
	hooks     writeHooks
}

// NewWriteExecutor 纯内存装配文件发布器。
func NewWriteExecutor(workspace *Workspace) (*WriteExecutor, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &WriteExecutor{workspace: workspace}, nil
}

// Execute 在打开、发布和清理三个边界保留确定的结果，不自动重试。
func (e *WriteExecutor) Execute(ctx context.Context, c tool.WriteCandidate) (result tool.InvocationResult) {
	failed := func(status tool.ResultStatus, code string) tool.InvocationResult {
		return tool.NewWriteCandidateResult(c, status, code)
	}
	if ctx == nil || ctx.Err() != nil {
		return failed(tool.ResultCancelled, "cancelled")
	}
	if e == nil || e.workspace == nil || c.Validate() != nil {
		return tool.NewWriteErrorResult(c.Invocation(), tool.ResultError, "invalid_candidate", "Write failed: invalid_candidate")
	}
	path, err := e.workspace.handle.openWritePath(c.RelativePath(), e.workspace.hooks)
	if err != nil {
		return failed(tool.ResultConflict, "write_conflict")
	}
	defer func() {
		if path.close() != nil {
			result = failed(tool.ResultOutcomeUncertain, "write_cleanup_uncertain")
		}
	}()
	state, err := path.inspect(ctx)
	if ctx.Err() != nil {
		return failed(tool.ResultCancelled, "cancelled")
	}
	if err != nil || !matchesWriteCandidate(state, c) {
		return failed(tool.ResultConflict, "write_conflict")
	}
	err = path.publish(ctx, c, e.hooks)
	switch {
	case err == nil:
		return failed(tool.ResultSuccess, "ok")
	case errors.Is(err, errWriteUncertain):
		return failed(tool.ResultOutcomeUncertain, "write_durability_uncertain")
	case errors.Is(err, errWriteConflict):
		return failed(tool.ResultConflict, "write_conflict")
	case errors.Is(err, errWriteUnsupported):
		return failed(tool.ResultError, "write_atomic_publish_unsupported")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return failed(tool.ResultCancelled, "cancelled")
	default:
		return failed(tool.ResultError, "write_failed")
	}
}

func matchesWriteCandidate(state writeInspection, c tool.WriteCandidate) bool {
	return state.operation == c.Operation() && state.base == c.Evidence() && slices.Equal(state.ancestors, c.Ancestors()) && slices.Equal(state.directories, c.Directories())
}

func safeWriteOpenError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return writeCancelledError()
	case errors.Is(err, errWriteConflict):
		return writeConflictError()
	case errors.Is(err, errWorkspaceUnsupported):
		return writeError("workspace_unsupported")
	case errors.Is(err, errTargetNotRegular):
		return writeError("not_regular_file")
	case errors.Is(err, errTargetNotReadable):
		return writeError("permission_denied")
	case errors.Is(err, errTargetTooLarge):
		return writeError("file_too_large")
	default:
		return writeError("write_target_unavailable")
	}
}
