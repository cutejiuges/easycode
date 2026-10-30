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
	ResultDenied           ResultStatus = "denied"
	ResultConflict         ResultStatus = "conflict"
	ResultError            ResultStatus = "error"
	ResultCancelled        ResultStatus = "cancelled"
	ResultOutcomeUncertain ResultStatus = "outcome_uncertain"
)

// Valid 判断结果状态是否受支持。
func (status ResultStatus) Valid() bool {
	switch status {
	case ResultSuccess, ResultDenied, ResultConflict, ResultError, ResultCancelled, ResultOutcomeUncertain:
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
	searchSaving   ArtifactSaving
	budget         ResultBudgetMetadata
	artifactInput  ReadArtifactInput
	artifactPage   ArtifactPage
	invocationID   InvocationID
	providerCallID ProviderCallID
	capability     CapabilityID
	wireKind       ProviderWireKind
	status         ResultStatus
	code           string
	preview        ModelPreview
	readMetadata   ReadResultMetadata
	globMetadata   GlobResultMetadata
	grepMetadata   GrepResultMetadata
	patchMetadata  PatchResultMetadata
	writeMetadata  WriteResultMetadata
	execMetadata   ExecResultMetadata
}

// NewReadInvocationResult 构造完整 Read 结果。
func NewReadInvocationResult(invocation ReadInvocation, status ResultStatus, code string, preview ModelPreview, metadata ReadResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityRead, wireKind: WireStructured, status: status, code: code, preview: preview, readMetadata: metadata,
	}
	return initializeResultBudget(result)
}

// NewGlobInvocationResult 构造完整 Glob 结果。
func NewGlobInvocationResult(invocation GlobInvocation, status ResultStatus, code string, preview ModelPreview, metadata GlobResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityGlob, wireKind: WireStructured, status: status, code: code, preview: preview, globMetadata: metadata,
	}
	return initializeResultBudget(result)
}

// NewGrepInvocationResult 构造完整 Grep 结果。
func NewGrepInvocationResult(invocation GrepInvocation, status ResultStatus, code string, preview ModelPreview, metadata GrepResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityGrep, wireKind: WireStructured, status: status, code: code, preview: preview, grepMetadata: metadata,
	}
	return initializeResultBudget(result)
}

// NewPatchInvocationResult 构造完整Patch结果并保留原生wire kind。
func NewPatchInvocationResult(invocation PatchInvocation, status ResultStatus, code string, preview ModelPreview, metadata PatchResultMetadata) (InvocationResult, error) {
	result := InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityPatch, wireKind: invocation.wireKind, status: status, code: code, preview: preview, patchMetadata: metadata,
	}
	return initializeResultBudget(result)
}

func (result InvocationResult) InvocationID() InvocationID       { return result.invocationID }
func (result InvocationResult) ProviderCallID() ProviderCallID   { return result.providerCallID }
func (result InvocationResult) Capability() CapabilityID         { return result.capability }
func (result InvocationResult) WireKind() ProviderWireKind       { return result.wireKind }
func (result InvocationResult) Status() ResultStatus             { return result.status }
func (result InvocationResult) Code() string                     { return result.code }
func (result InvocationResult) Preview() ModelPreview            { return result.preview }
func (result InvocationResult) ReadMetadata() ReadResultMetadata { return result.readMetadata }
func (result InvocationResult) GlobMetadata() GlobResultMetadata { return result.globMetadata }
func (result InvocationResult) GrepMetadata() GrepResultMetadata { return result.grepMetadata }
func (result InvocationResult) PatchMetadata() PatchResultMetadata {
	return result.patchMetadata
}

func (result InvocationResult) WriteMetadata() WriteResultMetadata { return result.writeMetadata }

// Validate 验证结果身份、状态、错误码、preview 和元数据。
func (result InvocationResult) Validate() error {
	if result.capability == CapabilityGlob || result.capability == CapabilityGrep {
		if (result.capability != CapabilityGlob && result.capability != CapabilityGrep) || result.searchSaving.Validate() != nil {
			return errors.New("invalid search saving")
		}
		if d, ok := result.searchSaving.Descriptor(); ok && (d.Stream() != ArtifactSearchResult || d.InvocationID() != result.invocationID) {
			return errors.New("search artifact mismatch")
		}
	} else if result.searchSaving != (ArtifactSaving{}) {
		return errors.New("mismatched search saving")
	}

	if result.budget.Validate() != nil || result.budget.actual != len(result.preview.text) || ((result.capability == CapabilityGlob || result.capability == CapabilityGrep) && result.budget.allowance > MaxSearchPreviewBytes) {
		return errors.New("invalid tool result budget")
	}
	if result.capability != CapabilityReadArtifact && (result.artifactInput != (ReadArtifactInput{}) || result.artifactPage != (ArtifactPage{})) {
		return errors.New("mismatched artifact result")
	}
	if result.capability != CapabilityExec && result.execMetadata != (ExecResultMetadata{}) {
		return errors.New("tool result contains mismatched Exec metadata")
	}
	if !result.invocationID.Valid() || !result.providerCallID.Valid() {
		return errors.New("tool result identity is invalid")
	}
	if !result.status.Valid() {
		return errors.New("tool result status is invalid")
	}
	if !result.wireKind.Valid() {
		return errors.New("tool result wire kind is invalid")
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
	if result.capability != CapabilityWrite && result.writeMetadata != (WriteResultMetadata{}) {
		return errors.New("tool result contains mismatched Write metadata")
	}
	switch result.capability {
	case CapabilityReadArtifact:
		return result.validateArtifactResult()
	case CapabilityExec:
		return result.validateExecResult()
	case CapabilityRead:
		if result.wireKind != WireStructured || !result.globMetadata.zero() || !result.grepMetadata.zero() || !result.patchMetadata.zero() {
			return errors.New("Read result contains mismatched metadata")
		}
		return result.readMetadata.Validate()
	case CapabilityGlob:
		if result.wireKind != WireStructured || result.readMetadata != (ReadResultMetadata{}) || !result.grepMetadata.zero() || !result.patchMetadata.zero() || len(result.preview.text) > MaxSearchPreviewBytes {
			return errors.New("Glob result contains mismatched metadata")
		}
		return result.globMetadata.Validate()
	case CapabilityGrep:
		if result.wireKind != WireStructured || result.readMetadata != (ReadResultMetadata{}) || !result.globMetadata.zero() || !result.patchMetadata.zero() || len(result.preview.text) > MaxSearchPreviewBytes {
			return errors.New("Grep result contains mismatched metadata")
		}
		return result.grepMetadata.Validate()
	case CapabilityPatch:
		if result.readMetadata != (ReadResultMetadata{}) || !result.globMetadata.zero() || !result.grepMetadata.zero() {
			return errors.New("Patch result contains mismatched metadata")
		}
		return result.patchMetadata.Validate()
	case CapabilityWrite:
		if result.wireKind != WireStructured || result.readMetadata != (ReadResultMetadata{}) || !result.globMetadata.zero() || !result.grepMetadata.zero() || !result.patchMetadata.zero() || (result.status == ResultSuccess && result.writeMetadata.operation == WriteOperationUnavailable) {
			return errors.New("Write result contains mismatched metadata")
		}
		return result.writeMetadata.Validate()
	default:
		return errors.New("tool result capability is invalid")
	}
}

func validResultCode(code string) bool {
	if len(code) > 128 {
		return false
	}
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

// NewInvocationErrorResult 为当前四种能力生成不泄漏底层cause的确定错误结果。
func NewInvocationErrorResult(invocation Invocation, status ResultStatus, code string, message string) InvocationResult {
	switch invocation.Capability() {
	case CapabilityReadArtifact:
		v, _ := invocation.ReadArtifact()
		p, _ := NewModelPreview(message)
		r, _ := NewReadArtifactInvocationResult(v, status, code, p, ArtifactPage{})
		return r
	case CapabilityExec:
		value, _ := invocation.Exec()
		return NewExecErrorResult(value, status, code, message)
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
	case CapabilityWrite:
		value, _ := invocation.Write()
		return NewWriteErrorResult(value, status, code, message)
	case CapabilityPatch:
		value, _ := invocation.Patch()
		return NewPatchErrorResult(value, status, code, message)
	}
	return InvocationResult{}
}

func invalidInternalResult(invocation ReadInvocation, relativePath string) InvocationResult {
	if relativePath == "" {
		relativePath = "."
	}
	preview := ModelPreview{text: "Read failed: internal_error"}
	metadata := ReadResultMetadata{relativePath: relativePath, requestedOffset: invocation.input.offset, requestedLimit: invocation.input.limit}
	result, _ := initializeResultBudget(InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityRead, wireKind: WireStructured, status: ResultError, code: "internal_error", preview: preview, readMetadata: metadata,
	})
	return result
}

// NewPatchSuccessResult 生成稳定且不包含文件正文的Patch成功结果。
func NewPatchSuccessResult(candidate PatchCandidate) InvocationResult {
	metadata := candidate.summary
	message := fmt.Sprintf("Updated %s (%d replacements, +%d -%d lines)", metadata.relativePath, metadata.replacementCount, metadata.addedLines, metadata.removedLines)
	preview, previewErr := NewModelPreview(message)
	invocation := PatchInvocation{
		invocationID: candidate.invocationID, providerCallID: candidate.providerCallID,
		wireKind: candidate.wireKind, input: candidate.input.Clone(),
	}
	if previewErr != nil {
		return invalidPatchInternalResult(invocation)
	}
	result, err := NewPatchInvocationResult(invocation, ResultSuccess, "ok", preview, metadata)
	if err != nil {
		return invalidPatchInternalResult(invocation)
	}
	return result
}

// NewPatchErrorResult 生成稳定Patch拒绝、冲突、取消或错误结果。
func NewPatchErrorResult(invocation PatchInvocation, status ResultStatus, code string, message string) InvocationResult {
	if status == ResultSuccess || !status.Valid() {
		status = ResultError
	}
	preview, previewErr := NewModelPreview(message)
	metadata, metadataErr := NewPatchResultMetadata(invocation.input.filePath, len(invocation.input.replacements), 0, 0)
	if previewErr != nil || metadataErr != nil {
		return invalidPatchInternalResult(invocation)
	}
	result, err := NewPatchInvocationResult(invocation, status, code, preview, metadata)
	if err != nil {
		return invalidPatchInternalResult(invocation)
	}
	return result
}

func invalidPatchInternalResult(invocation PatchInvocation) InvocationResult {
	path := invocation.input.filePath
	if path == "" || validateSearchPath(path) != nil {
		path = ".internal"
	}
	count := len(invocation.input.replacements)
	if count < 1 {
		count = 1
	}
	preview := ModelPreview{text: "Patch failed: internal_error"}
	metadata := PatchResultMetadata{relativePath: path, replacementCount: count}
	result, _ := initializeResultBudget(InvocationResult{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		capability: CapabilityPatch, wireKind: invocation.wireKind, status: ResultError,
		code: "internal_error", preview: preview, patchMetadata: metadata,
	})
	return result
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

// SearchSaving 返回搜索文本的冻结保存状态。
func (result InvocationResult) SearchSaving() ArtifactSaving {
	return result.searchSaving
}

// WithSearchSaving 仅在构造或恢复搜索结果时绑定保存事实。
func (result InvocationResult) WithSearchSaving(saving ArtifactSaving) (InvocationResult, error) {
	if saving.Validate() != nil {
		return InvocationResult{}, errors.New("invalid search saving")
	}
	result.searchSaving = saving
	return result, result.Validate()
}
