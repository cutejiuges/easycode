package builtin

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"easycode/internal/tool"
)

const maxReadFileBytes = 16 << 20

var (
	errWorkspaceUnsupported = errors.New("workspace security is unsupported")
	errTargetNotRegular     = errors.New("workspace target is not a regular file")
	errTargetNotReadable    = errors.New("workspace target is not readable")
	errTargetTooLarge       = errors.New("workspace target exceeds size limit")
)

// Workspace 是显式打开并由应用唯一持有的工作区目录 capability。
type Workspace struct {
	rootPath string
	handle   workspaceHandle
	close    sync.Once
	closeErr error
	hooks    workspaceHooks
}

type workspaceHooks struct {
	beforeOpenComponent func(string)
	afterOpenComponent  func(string)
}

// OpenWorkspace 冻结并打开启动工作区；调用方负责 Close。
func OpenWorkspace(rootPath string) (*Workspace, error) {
	absolute, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, errors.New("workspace path is invalid")
	}
	absolute = filepath.Clean(absolute)
	handle, err := openWorkspaceHandle(absolute)
	if err != nil {
		return nil, errors.New("workspace is unavailable")
	}
	return &Workspace{rootPath: absolute, handle: handle}, nil
}

// Close 幂等关闭工作区目录 handle。
func (workspace *Workspace) Close() error {
	if workspace == nil {
		return nil
	}
	workspace.close.Do(func() { workspace.closeErr = workspace.handle.close() })
	return workspace.closeErr
}

func (workspace *Workspace) relativePath(input string) (string, error) {
	if workspace == nil || workspace.handle == nil {
		return "", errors.New("workspace is closed")
	}
	cleaned := filepath.Clean(input)
	if filepath.IsAbs(cleaned) {
		relative, err := filepath.Rel(workspace.rootPath, cleaned)
		if err != nil {
			return "", errors.New("path is outside workspace")
		}
		cleaned = relative
	}
	if cleaned == "." || cleaned == "" || filepath.IsAbs(cleaned) {
		return "", errors.New("path is not a file")
	}
	components := strings.Split(filepath.ToSlash(cleaned), "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, 0) {
			return "", errors.New("path is outside workspace")
		}
	}
	return strings.Join(components, "/"), nil
}

// ReadExecutor 是只依赖已打开 Workspace capability 的 Read 执行器。
type ReadExecutor struct{ workspace *Workspace }

// NewReadExecutor 纯内存组合一个 Read executor，不访问文件系统。
func NewReadExecutor(workspace *Workspace) (*ReadExecutor, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &ReadExecutor{workspace: workspace}, nil
}

// Execute 读取 workspace 内经过 handle 约束的 UTF-8 普通文件。
func (executor *ReadExecutor) Execute(ctx context.Context, invocation tool.ReadInvocation) tool.InvocationResult {
	if ctx == nil || ctx.Err() != nil {
		return readError(invocation, tool.ResultCancelled, "cancelled", "Read cancelled", ".")
	}
	if executor == nil || executor.workspace == nil || invocation.Validate() != nil {
		return readError(invocation, tool.ResultError, "invalid_invocation", "Read failed: invalid invocation", ".")
	}
	relative, err := executor.workspace.relativePath(invocation.Input().FilePath())
	if err != nil {
		return readError(invocation, tool.ResultError, "path_outside_workspace", "Read failed: path is outside workspace", safeRelative(invocation.Input().FilePath()))
	}
	file, err := executor.workspace.handle.openFile(relative, executor.workspace.hooks)
	if err != nil {
		switch {
		case errors.Is(err, errWorkspaceUnsupported):
			return readError(invocation, tool.ResultError, "unsupported_platform", "Read failed: workspace security is unsupported", relative)
		case errors.Is(err, errTargetNotRegular):
			return readError(invocation, tool.ResultError, "unsupported_file_type", "Read failed: target is not a regular file", relative)
		case errors.Is(err, errTargetNotReadable):
			return readError(invocation, tool.ResultError, "permission_denied", "Read failed: file is not readable", relative)
		case errors.Is(err, errTargetTooLarge):
			return readError(invocation, tool.ResultError, "file_too_large", "Read failed: file exceeds 16 MiB", relative)
		}
		return readError(invocation, tool.ResultError, "file_unavailable", "Read failed: file is unavailable", relative)
	}
	defer func() { _ = file.Close() }()
	data, code, message := readBounded(ctx, file)
	if code != "" {
		status := tool.ResultError
		if code == "cancelled" {
			status = tool.ResultCancelled
		}
		return readError(invocation, status, code, message, relative)
	}
	if !utf8.Valid(data) || containsNUL(data) {
		return readError(invocation, tool.ResultError, "binary_file", "Read failed: file is not valid UTF-8 text", relative)
	}
	lines := splitTextLines(string(data))
	selected := selectLines(lines, invocation.Input().Offset(), invocation.Input().Limit())
	return tool.RenderReadSuccess(invocation, relative, selected, len(lines))
}

func readBounded(ctx context.Context, file io.Reader) ([]byte, string, string) {
	data := make([]byte, 0, 64<<10)
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, "cancelled", "Read cancelled"
		}
		count, err := file.Read(buffer)
		if count > 0 {
			if len(data)+count > maxReadFileBytes {
				return nil, "file_too_large", "Read failed: file exceeds 16 MiB"
			}
			data = append(data, buffer[:count]...)
		}
		if errors.Is(err, io.EOF) {
			return data, "", ""
		}
		if err != nil {
			return nil, "read_failed", "Read failed: file could not be read"
		}
	}
}

func splitTextLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}

func selectLines(lines []string, offset int, limit int) []string {
	start := offset - 1
	if start >= len(lines) {
		return []string{}
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	return append([]string(nil), lines[start:end]...)
}

func containsNUL(data []byte) bool {
	for _, value := range data {
		if value == 0 {
			return true
		}
	}
	return false
}

func safeRelative(value string) string {
	cleaned := filepath.Base(filepath.Clean(value))
	if cleaned == "." || cleaned == string(filepath.Separator) || cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func readError(invocation tool.ReadInvocation, status tool.ResultStatus, code string, message string, relative string) tool.InvocationResult {
	return tool.NewReadErrorResult(invocation, status, code, message, relative)
}
