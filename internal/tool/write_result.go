package tool

import (
	"errors"
	"fmt"
	"strings"
)

// WriteOperation 明确区分创建、覆盖和无法从已知事实确定的操作。
type WriteOperation string

const (
	WriteCreate               WriteOperation = "create"
	WriteOverwrite            WriteOperation = "overwrite"
	WriteOperationUnavailable WriteOperation = "unavailable"
)

func (operation WriteOperation) Valid() bool {
	return operation == WriteCreate || operation == WriteOverwrite || operation == WriteOperationUnavailable
}

// WriteResultMetadata 保存不含正文的冻结写入摘要。
type WriteResultMetadata struct {
	relativePath       string
	operation          WriteOperation
	bytes              int
	lines              int
	createdDirectories int
}

// NewWriteResultMetadata 构造明确操作与精确计数的结果元数据。
func NewWriteResultMetadata(path string, operation WriteOperation, byteCount, lineCount, directories int) (WriteResultMetadata, error) {
	m := WriteResultMetadata{relativePath: path, operation: operation, bytes: byteCount, lines: lineCount, createdDirectories: directories}
	return m, m.Validate()
}

func (m WriteResultMetadata) RelativePath() string      { return m.relativePath }
func (m WriteResultMetadata) Operation() WriteOperation { return m.operation }
func (m WriteResultMetadata) Bytes() int                { return m.bytes }
func (m WriteResultMetadata) Lines() int                { return m.lines }
func (m WriteResultMetadata) CreatedDirectories() int   { return m.createdDirectories }

// Validate 验证计数、操作和路径的内部一致性。
func (m WriteResultMetadata) Validate() error {
	if validateWritePath(m.relativePath) != nil || !m.operation.Valid() || m.bytes < 0 || m.bytes > MaxWriteInputBytes || m.lines < 0 || m.lines > m.bytes || (m.bytes == 0) != (m.lines == 0) || m.createdDirectories < 0 || m.createdDirectories > MaxWriteDirectories {
		return errors.New("invalid Write metadata")
	}
	if m.operation == WriteOperationUnavailable && (m.bytes != 0 || m.lines != 0 || m.createdDirectories != 0) || m.operation == WriteOverwrite && m.createdDirectories != 0 {
		return errors.New("Write operation does not match metadata")
	}
	return nil
}

// WriteLineCount 将末尾换行作为终止符，空内容为零行。
func WriteLineCount(content string) int {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return n
}

// NewWriteInvocationResult 构造可持久化的 Write 结果，不持有候选证据。
func NewWriteInvocationResult(v WriteInvocation, status ResultStatus, code string, preview ModelPreview, m WriteResultMetadata) (InvocationResult, error) {
	if v.Validate() != nil || m.relativePath != v.input.filePath {
		return InvocationResult{}, errors.New("Write result does not match invocation")
	}
	if status == ResultSuccess && (m.operation == WriteOperationUnavailable) {
		return InvocationResult{}, errors.New("Write success counts do not match input")
	}
	r := InvocationResult{invocationID: v.invocationID, providerCallID: v.providerCallID, capability: CapabilityWrite, wireKind: WireStructured, status: status, code: code, preview: preview, writeMetadata: m}
	return initializeResultBudget(r)
}

// NewWriteErrorResult 为准备失败和恢复补偿保存明确的不可确定操作。
func NewWriteErrorResult(v WriteInvocation, status ResultStatus, code, message string) InvocationResult {
	m, _ := NewWriteResultMetadata(v.input.filePath, WriteOperationUnavailable, 0, 0, 0)
	p, err := NewModelPreview(message)
	if err != nil || status == ResultSuccess {
		return InvocationResult{}
	}
	r, _ := NewWriteInvocationResult(v, status, code, p, m)
	return r
}

// NewWriteCandidateResult 冻结执行结果，成功只返回路径及计数。
func NewWriteCandidateResult(c WriteCandidate, status ResultStatus, code string) InvocationResult {
	m := c.summary
	message := "Write failed: " + code
	if status == ResultSuccess {
		verb := "Created"
		if c.operation == WriteOverwrite {
			verb = "Overwrote"
		}
		message = fmt.Sprintf("%s %s (%d bytes, %d lines, %d directories created)", verb, m.relativePath, m.bytes, m.lines, m.createdDirectories)
	} else {
		m.bytes, m.lines, m.createdDirectories = 0, 0, 0
	}
	p, _ := NewModelPreview(message)
	r, _ := NewWriteInvocationResult(c.invocation, status, code, p, m)
	return r
}
