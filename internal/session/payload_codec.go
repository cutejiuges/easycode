package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"easycode/internal/tool"
)

// DecodeSessionMetaPayload 严格解码并验证 session_meta v1 payload。
func DecodeSessionMetaPayload(record Record) (SessionMetaPayload, error) {
	payload, err := decodeKnownPayload[SessionMetaPayload](record, EventSessionMeta)
	if err != nil {
		return SessionMetaPayload{}, err
	}
	if err := validateSessionMetaPayload(payload); err != nil {
		return SessionMetaPayload{}, err
	}
	return payload, nil
}

// DecodeThreadMetaPayload 严格解码并验证 thread_meta v1 payload。
func DecodeThreadMetaPayload(record Record) (ThreadMetaPayload, error) {
	payload, err := decodeKnownPayload[ThreadMetaPayload](record, EventThreadMeta)
	if err != nil {
		return ThreadMetaPayload{}, err
	}
	if err := validateThreadMetaPayload(payload); err != nil {
		return ThreadMetaPayload{}, err
	}
	return payload, nil
}

// DecodeTurnStartedPayload 严格解码并验证 turn_started v1 payload。
func DecodeTurnStartedPayload(record Record) (TurnStartedPayload, error) {
	return decodeKnownPayload[TurnStartedPayload](record, EventTurnStarted)
}

// DecodeNativeCommitPayload 严格解码并验证 provider_native_commit v1 payload。
func DecodeNativeCommitPayload(record Record) (NativeCommitPayload, error) {
	payload, err := decodeKnownPayload[NativeCommitPayload](record, EventProviderNativeCommit)
	if err != nil {
		return NativeCommitPayload{}, err
	}
	if err := validateNativeCommitPayload(payload); err != nil {
		return NativeCommitPayload{}, err
	}
	payload.Payload = append(json.RawMessage(nil), payload.Payload...)
	return payload, nil
}

// DecodeSampleUsagePayload 严格解码并验证 sample_usage v1 payload。
func DecodeSampleUsagePayload(record Record) (SampleUsagePayload, error) {
	payload, err := decodeKnownPayload[SampleUsagePayload](record, EventSampleUsage)
	if err != nil {
		return SampleUsagePayload{}, err
	}
	if _, err := payload.Domain(); err != nil {
		return SampleUsagePayload{}, err
	}
	return payload, nil
}

// DecodeToolCallReadyPayload 严格解码并验证tool_call_ready payload。
func DecodeToolCallReadyPayload(record Record) (ToolCallReadyPayload, error) {
	payload, err := decodeKnownPayload[ToolCallReadyPayload](record, EventToolCallReady)
	if err != nil {
		return ToolCallReadyPayload{}, err
	}
	if _, err := payload.Domain(); err != nil {
		return ToolCallReadyPayload{}, err
	}
	return payload, nil
}

// Domain 重建已经分配 identity 的三工具 typed invocation。
func (payload ToolCallReadyPayload) Domain() (tool.Invocation, error) {
	if !payload.InvocationID.Valid() || !payload.ProviderCallID.Valid() || payload.SampleIndex >= 16 || payload.CallIndex >= 64 {
		return tool.Invocation{}, fmt.Errorf("tool call ready payload is invalid")
	}
	var ready tool.ReadyCall
	var err error
	switch payload.Capability {
	case tool.CapabilityRead:
		if payload.ReadInput == nil || payload.GlobInput != nil || payload.GrepInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Read capability")
		}
		input, inputErr := tool.NewReadInput(payload.ReadInput.FilePath, payload.ReadInput.Offset, payload.ReadInput.Limit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Read input is invalid: %w", inputErr)
		}
		ready, err = tool.NewReadReadyCall(payload.ProviderCallID, input)
	case tool.CapabilityGlob:
		if payload.GlobInput == nil || payload.ReadInput != nil || payload.GrepInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Glob capability")
		}
		input, inputErr := tool.NewGlobInput(payload.GlobInput.Pattern, payload.GlobInput.Path, payload.GlobInput.Limit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Glob input is invalid: %w", inputErr)
		}
		ready, err = tool.NewGlobReadyCall(payload.ProviderCallID, input)
	case tool.CapabilityGrep:
		if payload.GrepInput == nil || payload.ReadInput != nil || payload.GlobInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Grep capability")
		}
		value := payload.GrepInput
		input, inputErr := tool.NewGrepInput(value.Pattern, value.Path, value.Glob, value.OutputMode, value.CaseInsensitive, value.BeforeContext, value.AfterContext, value.Limit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Grep input is invalid: %w", inputErr)
		}
		ready, err = tool.NewGrepReadyCall(payload.ProviderCallID, input)
	default:
		return tool.Invocation{}, fmt.Errorf("tool call ready capability is invalid")
	}
	if err != nil {
		return tool.Invocation{}, fmt.Errorf("tool call ready identity is invalid: %w", err)
	}
	return tool.NewInvocation(payload.InvocationID, ready)
}

// DecodeToolExecutionStartedPayload 严格解码并验证tool_execution_started payload。
func DecodeToolExecutionStartedPayload(record Record) (ToolExecutionStartedPayload, error) {
	payload, err := decodeKnownPayload[ToolExecutionStartedPayload](record, EventToolExecutionStarted)
	if err != nil {
		return ToolExecutionStartedPayload{}, err
	}
	if err := validateToolExecutionStartedPayload(payload); err != nil {
		return ToolExecutionStartedPayload{}, err
	}
	return payload, nil
}

func validateToolExecutionStartedPayload(payload ToolExecutionStartedPayload) error {
	if !payload.InvocationID.Valid() {
		return fmt.Errorf("tool execution started payload is invalid")
	}
	return nil
}

// DecodeToolCallResultPayload 严格解码并验证tool_call_result payload。
func DecodeToolCallResultPayload(record Record) (ToolCallResultPayload, error) {
	payload, err := decodeKnownPayload[ToolCallResultPayload](record, EventToolCallResult)
	if err != nil {
		return ToolCallResultPayload{}, err
	}
	if err := validateToolCallResultPayload(payload); err != nil {
		return ToolCallResultPayload{}, err
	}
	return payload, nil
}

func validateToolCallResultPayload(payload ToolCallResultPayload) error {
	if !payload.InvocationID.Valid() || !payload.Capability.Valid() || !payload.Status.Valid() || payload.Code == "" || !utf8.ValidString(payload.Preview) {
		return fmt.Errorf("tool call result payload is invalid")
	}
	invocation, err := validationInvocation(payload)
	if err != nil {
		return err
	}
	_, err = payload.Domain(invocation)
	return err
}

// Domain 使用对应ready invocation重建冻结工具结果。
func (payload ToolCallResultPayload) Domain(invocation tool.Invocation) (tool.InvocationResult, error) {
	if err := invocation.Validate(); err != nil || invocation.InvocationID() != payload.InvocationID || invocation.Capability() != payload.Capability {
		return tool.InvocationResult{}, fmt.Errorf("tool call result invocation is invalid")
	}
	preview, err := tool.NewModelPreview(payload.Preview)
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result preview is invalid: %w", err)
	}
	var result tool.InvocationResult
	switch payload.Capability {
	case tool.CapabilityRead:
		result, err = decodeReadResult(payload, invocation, preview)
	case tool.CapabilityGlob:
		result, err = decodeGlobResult(payload, invocation, preview)
	case tool.CapabilityGrep:
		result, err = decodeGrepResult(payload, invocation, preview)
	default:
		return tool.InvocationResult{}, fmt.Errorf("tool call result capability is invalid")
	}
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result payload is invalid: %w", err)
	}
	return result, nil
}

func validationInvocation(payload ToolCallResultPayload) (tool.Invocation, error) {
	callID, _ := tool.ParseProviderCallID("payload-validation")
	var ready tool.ReadyCall
	var err error
	switch payload.Capability {
	case tool.CapabilityRead:
		if payload.ReadMetadata == nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Read capability")
		}
		input, inputErr := tool.NewReadInput(".", payload.ReadMetadata.RequestedOffset, payload.ReadMetadata.RequestedLimit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result Read metadata is invalid: %w", inputErr)
		}
		ready, err = tool.NewReadReadyCall(callID, input)
	case tool.CapabilityGlob:
		if payload.GlobMetadata == nil || payload.ReadMetadata != nil || payload.GrepMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Glob capability")
		}
		input, _ := tool.NewGlobInput("**", "", 1)
		ready, err = tool.NewGlobReadyCall(callID, input)
	case tool.CapabilityGrep:
		if payload.GrepMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Grep capability")
		}
		input, inputErr := tool.NewGrepInput("x", "", "", payload.GrepMetadata.Mode, false, 0, 0, 1)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result Grep metadata is invalid: %w", inputErr)
		}
		ready, err = tool.NewGrepReadyCall(callID, input)
	default:
		return tool.Invocation{}, fmt.Errorf("tool call result capability is invalid")
	}
	if err != nil {
		return tool.Invocation{}, err
	}
	return tool.NewInvocation(payload.InvocationID, ready)
}

func decodeReadResult(payload ToolCallResultPayload, invocation tool.Invocation, preview tool.ModelPreview) (tool.InvocationResult, error) {
	if payload.ReadMetadata == nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil {
		return tool.InvocationResult{}, fmt.Errorf("read metadata union is invalid")
	}
	value, ok := invocation.Read()
	if !ok {
		return tool.InvocationResult{}, fmt.Errorf("read invocation is required")
	}
	input := value.Input()
	metadataPayload := payload.ReadMetadata
	if input.Offset() != metadataPayload.RequestedOffset || input.Limit() != metadataPayload.RequestedLimit {
		return tool.InvocationResult{}, fmt.Errorf("read request metadata does not match invocation")
	}
	metadata, err := tool.NewReadResultMetadata(
		metadataPayload.RelativePath, input, metadataPayload.StartLine, metadataPayload.EndLine,
		metadataPayload.ReachedEOF, metadataPayload.LongLineTruncated, metadataPayload.OutputTruncated,
	)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return tool.NewReadInvocationResult(value, payload.Status, payload.Code, preview, metadata)
}

func decodeGlobResult(payload ToolCallResultPayload, invocation tool.Invocation, preview tool.ModelPreview) (tool.InvocationResult, error) {
	if payload.GlobMetadata == nil || payload.ReadMetadata != nil || payload.GrepMetadata != nil {
		return tool.InvocationResult{}, fmt.Errorf("glob metadata union is invalid")
	}
	value, ok := invocation.Glob()
	if !ok {
		return tool.InvocationResult{}, fmt.Errorf("glob invocation is required")
	}
	metadataPayload := payload.GlobMetadata
	skipped, err := decodeSkipCounts(metadataPayload.Skipped)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	metadata, err := tool.NewGlobResultMetadata(
		metadataPayload.Matches, metadataPayload.Truncated, metadataPayload.OmittedMatches,
		metadataPayload.VisitedEntries, metadataPayload.IncompleteReason, skipped,
	)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return tool.NewGlobInvocationResult(value, payload.Status, payload.Code, preview, metadata)
}

func decodeGrepResult(payload ToolCallResultPayload, invocation tool.Invocation, preview tool.ModelPreview) (tool.InvocationResult, error) {
	if payload.GrepMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil {
		return tool.InvocationResult{}, fmt.Errorf("grep metadata union is invalid")
	}
	value, ok := invocation.Grep()
	if !ok {
		return tool.InvocationResult{}, fmt.Errorf("grep invocation is required")
	}
	metadataPayload := payload.GrepMetadata
	skipped, err := decodeSkipCounts(metadataPayload.Skipped)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	matches := make([]tool.GrepMatch, len(metadataPayload.Matches))
	for index, match := range metadataPayload.Matches {
		switch metadataPayload.Mode {
		case tool.GrepOutputContent:
			matches[index], err = tool.NewGrepContentMatch(match.RelativePath, match.Line, match.Text, match.MatchingLine)
		case tool.GrepOutputFilesWithMatches:
			if match.Line != 0 || match.Text != "" || match.MatchingLine || match.Count != 0 {
				err = fmt.Errorf("grep file match payload is invalid")
			} else {
				matches[index], err = tool.NewGrepFileMatch(match.RelativePath)
			}
		case tool.GrepOutputCount:
			if match.Line != 0 || match.Text != "" || match.MatchingLine {
				err = fmt.Errorf("grep count match payload is invalid")
			} else {
				matches[index], err = tool.NewGrepCountMatch(match.RelativePath, match.Count)
			}
		default:
			err = fmt.Errorf("grep output mode is invalid")
		}
		if err != nil {
			return tool.InvocationResult{}, err
		}
	}
	metadata, err := tool.NewGrepResultMetadata(
		metadataPayload.Mode, matches, metadataPayload.MatchingLines, metadataPayload.Truncated,
		metadataPayload.OmittedMatches, metadataPayload.VisitedEntries, metadataPayload.ScannedFiles,
		metadataPayload.ScannedBytes, metadataPayload.IncompleteReason, skipped,
	)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return tool.NewGrepInvocationResult(value, payload.Status, payload.Code, preview, metadata)
}

func decodeSkipCounts(payload SearchSkipCountsPayload) (tool.SearchSkipCounts, error) {
	return tool.NewSearchSkipCounts(payload.Binary, payload.InvalidUTF8, payload.TooLarge, payload.Unreadable, payload.Unsupported, payload.Disappeared)
}

func encodeSkipCounts(counts tool.SearchSkipCounts) SearchSkipCountsPayload {
	return SearchSkipCountsPayload{
		Binary: counts.Binary(), InvalidUTF8: counts.InvalidUTF8(), TooLarge: counts.TooLarge(),
		Unreadable: counts.Unreadable(), Unsupported: counts.Unsupported(), Disappeared: counts.Disappeared(),
	}
}

func encodeGlobMetadata(metadata tool.GlobResultMetadata) *GlobResultMetadataPayload {
	return &GlobResultMetadataPayload{
		Matches: metadata.Matches(), Truncated: metadata.Truncated(), OmittedMatches: metadata.OmittedMatches(),
		VisitedEntries: metadata.VisitedEntries(), IncompleteReason: metadata.IncompleteReason(), Skipped: encodeSkipCounts(metadata.Skipped()),
	}
}

func encodeGrepMetadata(metadata tool.GrepResultMetadata) *GrepResultMetadataPayload {
	matches := metadata.Matches()
	payloadMatches := make([]GrepMatchPayload, len(matches))
	for index, match := range matches {
		payloadMatches[index] = GrepMatchPayload{
			RelativePath: match.RelativePath(), Line: match.Line(), Text: match.Text(), MatchingLine: match.MatchingLine(), Count: match.Count(),
		}
	}
	return &GrepResultMetadataPayload{
		Mode: metadata.Mode(), Matches: payloadMatches, MatchingLines: metadata.MatchingLines(),
		Truncated: metadata.Truncated(), OmittedMatches: metadata.OmittedMatches(), VisitedEntries: metadata.VisitedEntries(),
		ScannedFiles: metadata.ScannedFiles(), ScannedBytes: metadata.ScannedBytes(),
		IncompleteReason: metadata.IncompleteReason(), Skipped: encodeSkipCounts(metadata.Skipped()),
	}
}

// DecodeTurnCompletedPayload 严格解码并验证 turn_completed v1 payload。
func DecodeTurnCompletedPayload(record Record) (TurnCompletedPayload, error) {
	return decodeKnownPayload[TurnCompletedPayload](record, EventTurnCompleted)
}

// DecodeTurnFailedPayload 严格解码并验证 turn_failed v1 payload。
func DecodeTurnFailedPayload(record Record) (TurnFailedPayload, error) {
	payload, err := decodeKnownPayload[TurnFailedPayload](record, EventTurnFailed)
	if err != nil {
		return TurnFailedPayload{}, err
	}
	if err := validateTurnFailedPayload(payload); err != nil {
		return TurnFailedPayload{}, err
	}
	return payload, nil
}

func decodeKnownPayload[T recordPayload](record Record, kind EventKind) (T, error) {
	var zero T
	_, exists := descriptorByKind(kind)
	if !exists || record.EventKind != kind || record.PayloadVersion != EnvelopeVersion {
		return zero, fmt.Errorf("session payload declaration is invalid")
	}
	if err := rejectDuplicateJSONFields(record.Payload); err != nil {
		return zero, err
	}
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&zero); err != nil {
		return zero, fmt.Errorf("decode session payload: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return zero, fmt.Errorf("session payload contains trailing JSON")
	}
	return zero, nil
}

func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode session payload: %w", err)
		}
		delimiter, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				nameToken, nameErr := decoder.Token()
				name, ok := nameToken.(string)
				if nameErr != nil || !ok {
					return fmt.Errorf("decode session payload object")
				}
				if _, exists := seen[name]; exists {
					return fmt.Errorf("session payload contains duplicate field %q", name)
				}
				seen[name] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return fmt.Errorf("decode session payload object")
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return fmt.Errorf("decode session payload array")
			}
		default:
			return fmt.Errorf("decode session payload delimiter")
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("session payload contains trailing JSON")
	}
	return nil
}

func validateSessionMetaPayload(payload SessionMetaPayload) error {
	if !payload.RootThreadID.Valid() || payload.CreatedAt.IsZero() || payload.CreatedAt.Location() != time.UTC ||
		!payload.Provider.Valid() || strings.TrimSpace(payload.ProviderWire) == "" || strings.TrimSpace(payload.Model) == "" {
		return fmt.Errorf("session metadata payload is invalid")
	}
	if !filepath.IsAbs(payload.CreationCWD) || filepath.Clean(payload.CreationCWD) != payload.CreationCWD {
		return fmt.Errorf("session creation cwd is invalid")
	}
	return nil
}

func validateThreadMetaPayload(payload ThreadMetaPayload) error {
	if payload.Root {
		if payload.ParentThreadID != "" {
			return fmt.Errorf("root thread metadata parent must be empty")
		}
		return nil
	}
	if !payload.ParentThreadID.Valid() {
		return fmt.Errorf("child thread metadata parent is invalid")
	}
	return nil
}

func validateNativeCommitPayload(payload NativeCommitPayload) error {
	if !payload.Provider.Valid() || strings.TrimSpace(payload.Wire) == "" || payload.PayloadVersion <= 0 ||
		len(payload.Payload) == 0 || len(payload.Payload) > MaxRecordBytes || !json.Valid(payload.Payload) {
		return fmt.Errorf("provider native commit payload is invalid")
	}
	return nil
}

func validateTurnFailedPayload(payload TurnFailedPayload) error {
	if strings.TrimSpace(payload.Code) == "" {
		return fmt.Errorf("turn failure code is required")
	}
	return nil
}
