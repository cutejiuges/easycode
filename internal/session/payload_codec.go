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

// Domain 重建已经分配identity的typed Read invocation。
func (payload ToolCallReadyPayload) Domain() (tool.ReadInvocation, error) {
	if !payload.InvocationID.Valid() || !payload.ProviderCallID.Valid() || payload.SampleIndex >= 16 || payload.CallIndex >= 64 ||
		payload.Capability != tool.CapabilityRead || payload.InputRevision != tool.ReadInputRevision {
		return tool.ReadInvocation{}, fmt.Errorf("tool call ready payload is invalid")
	}
	input, err := tool.NewReadInput(payload.Input.FilePath, payload.Input.Offset, payload.Input.Limit)
	if err != nil {
		return tool.ReadInvocation{}, fmt.Errorf("tool call ready input is invalid: %w", err)
	}
	ready, err := tool.NewReadyCall(payload.ProviderCallID, input)
	if err != nil {
		return tool.ReadInvocation{}, fmt.Errorf("tool call ready identity is invalid: %w", err)
	}
	return tool.NewReadInvocation(payload.InvocationID, ready)
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
	if !payload.InvocationID.Valid() || !payload.Status.Valid() || payload.Code == "" ||
		payload.ResultCodecRevision != tool.ReadResultCodecRevision || !utf8.ValidString(payload.Preview) {
		return fmt.Errorf("tool call result payload is invalid")
	}
	input, err := tool.NewReadInput(".", payload.Metadata.RequestedOffset, payload.Metadata.RequestedLimit)
	if err != nil {
		return fmt.Errorf("tool call result metadata is invalid: %w", err)
	}
	callID, _ := tool.ParseProviderCallID("payload-validation")
	ready, _ := tool.NewReadyCall(callID, input)
	invocation, _ := tool.NewReadInvocation(payload.InvocationID, ready)
	_, err = payload.Domain(invocation)
	return err
}

// Domain 使用对应ready invocation重建冻结工具结果。
func (payload ToolCallResultPayload) Domain(invocation tool.ReadInvocation) (tool.InvocationResult, error) {
	if err := invocation.Validate(); err != nil || invocation.InvocationID() != payload.InvocationID ||
		payload.ResultCodecRevision != tool.ReadResultCodecRevision {
		return tool.InvocationResult{}, fmt.Errorf("tool call result invocation is invalid")
	}
	input := invocation.Input()
	if input.Offset() != payload.Metadata.RequestedOffset || input.Limit() != payload.Metadata.RequestedLimit {
		return tool.InvocationResult{}, fmt.Errorf("tool call result request metadata does not match invocation")
	}
	preview, err := tool.NewModelPreview(payload.Preview)
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result preview is invalid: %w", err)
	}
	metadata, err := tool.NewReadResultMetadata(
		payload.Metadata.RelativePath, input,
		payload.Metadata.StartLine, payload.Metadata.EndLine, payload.Metadata.ReachedEOF,
		payload.Metadata.LongLineTruncated, payload.Metadata.OutputTruncated,
	)
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result metadata is invalid: %w", err)
	}
	result, err := tool.NewInvocationResult(invocation, payload.Status, payload.Code, preview, metadata)
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result payload is invalid: %w", err)
	}
	return result, nil
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
	descriptor, exists := descriptorByKind(kind)
	if !exists || record.EventKind != kind || record.PayloadVersion != descriptor.Version ||
		record.ReplayRequirement != descriptor.Requirement {
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
		!payload.Provider.Valid() || strings.TrimSpace(payload.ProviderWire) == "" || strings.TrimSpace(payload.Model) == "" ||
		payload.SchemaRevision <= 0 {
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
