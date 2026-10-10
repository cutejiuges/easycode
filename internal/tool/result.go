package tool

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxReadLineCodePoints = 2000

// ResultStatus 是工具调用的封闭终态。
type ResultStatus string

const (
	ResultSuccess          ResultStatus = "success"
	ResultError            ResultStatus = "error"
	ResultCancelled        ResultStatus = "cancelled"
	ResultOutcomeUncertain ResultStatus = "outcome_uncertain"
)

// Valid 判断结果状态是否受支持。
func (status ResultStatus) Valid() bool {
	switch status {
	case ResultSuccess, ResultError, ResultCancelled, ResultOutcomeUncertain:
		return true
	default:
		return false
	}
}

// ModelPreview 保存冻结且有界的模型可见 UTF-8 文本。
type ModelPreview struct{ text string }

// NewModelPreview 构造模型可见结果。
func NewModelPreview(text string) (ModelPreview, error) {
	if !utf8.ValidString(text) || len(text) > MaxModelPreviewBytes {
		return ModelPreview{}, errors.New("model preview is invalid or exceeds 256 KiB")
	}
	return ModelPreview{text: text}, nil
}

// Text 返回不可变字符串。
func (preview ModelPreview) Text() string { return preview.text }

// Validate 验证结果预算和编码。
func (preview ModelPreview) Validate() error {
	_, err := NewModelPreview(preview.text)
	return err
}

// ReadResultMetadata 保存不含文件正文和绝对路径的恢复元数据。
type ReadResultMetadata struct {
	relativePath      string
	requestedOffset   int
	requestedLimit    int
	startLine         int
	endLine           int
	reachedEOF        bool
	longLineTruncated bool
	outputTruncated   bool
}

// NewReadResultMetadata 构造 Read 结果元数据。
func NewReadResultMetadata(relativePath string, input ReadInput, startLine int, endLine int, reachedEOF bool, longLineTruncated bool, outputTruncated bool) (ReadResultMetadata, error) {
	metadata := ReadResultMetadata{
		relativePath: relativePath, requestedOffset: input.offset, requestedLimit: input.limit,
		startLine: startLine, endLine: endLine, reachedEOF: reachedEOF,
		longLineTruncated: longLineTruncated, outputTruncated: outputTruncated,
	}
	if err := metadata.Validate(); err != nil {
		return ReadResultMetadata{}, err
	}
	return metadata, nil
}

func (metadata ReadResultMetadata) Validate() error {
	if metadata.relativePath == "" || !utf8.ValidString(metadata.relativePath) {
		return errors.New("read result relative path is invalid")
	}
	if metadata.requestedOffset < 1 || metadata.requestedLimit < 1 || metadata.requestedLimit > maxReadLines {
		return errors.New("read result request range is invalid")
	}
	if metadata.startLine < 0 || metadata.endLine < 0 || (metadata.endLine > 0 && metadata.endLine < metadata.startLine) {
		return errors.New("read result line range is invalid")
	}
	return nil
}

func (metadata ReadResultMetadata) RelativePath() string    { return metadata.relativePath }
func (metadata ReadResultMetadata) RequestedOffset() int    { return metadata.requestedOffset }
func (metadata ReadResultMetadata) RequestedLimit() int     { return metadata.requestedLimit }
func (metadata ReadResultMetadata) StartLine() int          { return metadata.startLine }
func (metadata ReadResultMetadata) EndLine() int            { return metadata.endLine }
func (metadata ReadResultMetadata) ReachedEOF() bool        { return metadata.reachedEOF }
func (metadata ReadResultMetadata) LongLineTruncated() bool { return metadata.longLineTruncated }
func (metadata ReadResultMetadata) OutputTruncated() bool   { return metadata.outputTruncated }

// InvocationResult 是可直接持久化并交给 Provider result codec 的冻结结果。
type InvocationResult struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	capability     CapabilityID
	status         ResultStatus
	code           string
	preview        ModelPreview
	readMetadata   ReadResultMetadata
	globMetadata   GlobResultMetadata
	grepMetadata   GrepResultMetadata
}

// NewReadInvocationResult 构造完整 Read 结果。
func NewReadInvocationResult(invocation ReadInvocation, status ResultStatus, code string, preview ModelPreview, metadata ReadResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityRead, status: status, code: code, preview: preview, readMetadata: metadata,
	}
	if err := result.Validate(); err != nil {
		return InvocationResult{}, err
	}
	return result, nil
}

// NewGlobInvocationResult 构造完整 Glob 结果。
func NewGlobInvocationResult(invocation GlobInvocation, status ResultStatus, code string, preview ModelPreview, metadata GlobResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityGlob, status: status, code: code, preview: preview, globMetadata: metadata,
	}
	if err := result.Validate(); err != nil {
		return InvocationResult{}, err
	}
	return result, nil
}

// NewGrepInvocationResult 构造完整 Grep 结果。
func NewGrepInvocationResult(invocation GrepInvocation, status ResultStatus, code string, preview ModelPreview, metadata GrepResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityGrep, status: status, code: code, preview: preview, grepMetadata: metadata,
	}
	if err := result.Validate(); err != nil {
		return InvocationResult{}, err
	}
	return result, nil
}

func (result InvocationResult) InvocationID() InvocationID       { return result.invocationID }
func (result InvocationResult) ProviderCallID() ProviderCallID   { return result.providerCallID }
func (result InvocationResult) Capability() CapabilityID         { return result.capability }
func (result InvocationResult) Status() ResultStatus             { return result.status }
func (result InvocationResult) Code() string                     { return result.code }
func (result InvocationResult) Preview() ModelPreview            { return result.preview }
func (result InvocationResult) ReadMetadata() ReadResultMetadata { return result.readMetadata }
func (result InvocationResult) GlobMetadata() GlobResultMetadata { return result.globMetadata }
func (result InvocationResult) GrepMetadata() GrepResultMetadata { return result.grepMetadata }

// Validate 验证结果身份、状态、错误码、preview 和元数据。
func (result InvocationResult) Validate() error {
	if !result.invocationID.Valid() || !result.providerCallID.Valid() {
		return errors.New("tool result identity is invalid")
	}
	if !result.status.Valid() {
		return errors.New("tool result status is invalid")
	}
	if result.code == "" || !validResultCode(result.code) {
		return errors.New("tool result code is invalid")
	}
	if result.status == ResultSuccess && result.code != "ok" {
		return errors.New("successful tool result must use ok code")
	}
	if err := result.preview.Validate(); err != nil {
		return err
	}
	switch result.capability {
	case CapabilityRead:
		if !result.globMetadata.zero() || !result.grepMetadata.zero() {
			return errors.New("Read result contains mismatched metadata")
		}
		return result.readMetadata.Validate()
	case CapabilityGlob:
		if result.readMetadata != (ReadResultMetadata{}) || !result.grepMetadata.zero() || len(result.preview.text) > MaxSearchPreviewBytes {
			return errors.New("Glob result contains mismatched metadata")
		}
		return result.globMetadata.Validate()
	case CapabilityGrep:
		if result.readMetadata != (ReadResultMetadata{}) || !result.globMetadata.zero() || len(result.preview.text) > MaxSearchPreviewBytes {
			return errors.New("Grep result contains mismatched metadata")
		}
		return result.grepMetadata.Validate()
	default:
		return errors.New("tool result capability is invalid")
	}
}

func validResultCode(code string) bool {
	for _, character := range code {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

// RenderReadSuccess 以稳定行号、长行上限和总预算渲染 Read 成功结果。
func RenderReadSuccess(invocation ReadInvocation, relativePath string, lines []string, totalLines int) InvocationResult {
	input := invocation.input
	start := input.offset
	if start > totalLines {
		start = 0
	}
	end := 0
	if len(lines) > 0 {
		end = input.offset + len(lines) - 1
	}
	reachedEOF := end >= totalLines || totalLines == 0 || input.offset > totalLines
	var builder strings.Builder
	longTruncated := false
	budgetTruncated := false
	for index, original := range lines {
		line, truncated := truncateRunes(original, maxReadLineCodePoints)
		longTruncated = longTruncated || truncated
		if truncated {
			line += "… [line truncated]"
		}
		encoded := strconv.Itoa(input.offset+index) + "\t" + line + "\n"
		if builder.Len()+len(encoded) > MaxModelPreviewBytes {
			budgetTruncated = true
			reachedEOF = false
			end = input.offset + index - 1
			break
		}
		builder.WriteString(encoded)
	}
	if budgetTruncated {
		marker := "[output truncated]\n"
		if builder.Len()+len(marker) <= MaxModelPreviewBytes {
			builder.WriteString(marker)
		}
	}
	preview, _ := NewModelPreview(builder.String())
	metadata, metadataErr := NewReadResultMetadata(relativePath, input, start, end, reachedEOF, longTruncated, budgetTruncated)
	if metadataErr != nil {
		return invalidInternalResult(invocation, relativePath)
	}
	result, resultErr := NewReadInvocationResult(invocation, ResultSuccess, "ok", preview, metadata)
	if resultErr != nil {
		return invalidInternalResult(invocation, relativePath)
	}
	return result
}

// NewReadErrorResult 生成不泄漏底层 cause 的确定错误结果。
func NewReadErrorResult(invocation ReadInvocation, status ResultStatus, code string, message string, relativePath string) InvocationResult {
	if status == ResultSuccess || !status.Valid() {
		status = ResultError
	}
	preview, previewErr := NewModelPreview(message)
	metadata, metadataErr := NewReadResultMetadata(relativePath, invocation.input, 0, 0, false, false, false)
	if previewErr != nil || metadataErr != nil {
		return invalidInternalResult(invocation, ".")
	}
	result, err := NewReadInvocationResult(invocation, status, code, preview, metadata)
	if err != nil {
		return invalidInternalResult(invocation, ".")
	}
	return result
}

// NewInvocationErrorResult 为三种能力生成不泄漏底层 cause 的确定错误结果。
func NewInvocationErrorResult(invocation Invocation, status ResultStatus, code string, message string) InvocationResult {
	switch invocation.Capability() {
	case CapabilityRead:
		value, _ := invocation.Read()
		return NewReadErrorResult(value, status, code, message, ".")
	case CapabilityGlob:
		value, _ := invocation.Glob()
		preview, _ := NewModelPreview(message)
		metadata, _ := NewGlobResultMetadata([]string{}, false, 0, 0, SearchComplete, SearchSkipCounts{})
		result, err := NewGlobInvocationResult(value, status, code, preview, metadata)
		if err == nil {
			return result
		}
	case CapabilityGrep:
		value, _ := invocation.Grep()
		preview, _ := NewModelPreview(message)
		metadata, _ := NewGrepResultMetadata(value.Input().OutputMode(), []GrepMatch{}, 0, false, 0, 0, 0, 0, SearchComplete, SearchSkipCounts{})
		result, err := NewGrepInvocationResult(value, status, code, preview, metadata)
		if err == nil {
			return result
		}
	}
	return InvocationResult{}
}

func invalidInternalResult(invocation ReadInvocation, relativePath string) InvocationResult {
	if relativePath == "" {
		relativePath = "."
	}
	preview := ModelPreview{text: "Read failed: internal_error"}
	metadata := ReadResultMetadata{relativePath: relativePath, requestedOffset: invocation.input.offset, requestedLimit: invocation.input.limit}
	return InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityRead, status: ResultError, code: "internal_error", preview: preview, readMetadata: metadata,
	}
}

func truncateRunes(value string, limit int) (string, bool) {
	if utf8.RuneCountInString(value) <= limit {
		return value, false
	}
	count := 0
	for index := range value {
		if count == limit {
			return value[:index], true
		}
		count++
	}
	return value, false
}

// ResultValidationError 提供不包含结果正文的稳定验证错误。
func ResultValidationError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("invalid tool result")
}
