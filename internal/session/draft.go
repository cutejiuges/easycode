package session

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// NewSessionMetaDraft 创建已验证并编码的当前 session_meta draft。
func NewSessionMetaDraft(payload SessionMetaPayload) (RecordDraft, error) {
	if err := validateSessionMetaPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventSessionMeta, "", "", payload)
}

// NewThreadMetaDraft 创建已验证并编码的当前 thread_meta draft。
func NewThreadMetaDraft(payload ThreadMetaPayload) (RecordDraft, error) {
	if err := validateThreadMetaPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventThreadMeta, payload.ParentThreadID, "", payload)
}

// NewTurnStartedDraft 创建已验证并编码的当前 turn_started draft。
func NewTurnStartedDraft(turnID domain.TurnID) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnStarted, "", turnID, TurnStartedPayload{})
}

// NewProviderNativeCommitDraft 创建已验证并编码的当前 provider_native_commit draft。
func NewProviderNativeCommitDraft(turnID domain.TurnID, payload NativeCommitPayload) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := validateNativeCommitPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	payload.Payload = append(json.RawMessage(nil), payload.Payload...)
	return encodeDraft(EventProviderNativeCommit, "", turnID, payload)
}

// NewSampleUsageDraft 创建已验证并编码的当前 sample_usage draft。
func NewSampleUsageDraft(turnID domain.TurnID, usage domain.SampleUsage) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	payload, err := NewSampleUsagePayload(usage)
	if err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventSampleUsage, "", turnID, payload)
}

// NewToolCallReadyDraft 创建完整参数已经durable前待写入的调用事实。
func NewToolCallReadyDraft(turnID domain.TurnID, invocation tool.Invocation, sampleIndex uint32, callIndex uint32) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := invocation.Validate(); err != nil {
		return RecordDraft{}, fmt.Errorf("tool invocation is invalid: %w", err)
	}
	payload := ToolCallReadyPayload{
		InvocationID: invocation.InvocationID(), ProviderCallID: invocation.ProviderCallID(),
		SampleIndex: sampleIndex, CallIndex: callIndex, Capability: invocation.Capability(), WireKind: invocation.WireKind(),
	}
	switch invocation.Capability() {
	case tool.CapabilityExec:
		value, _ := invocation.Exec()
		input := value.Input()
		payload.ExecInput = &ExecInputPayload{Command: input.Command(), Workdir: input.Workdir(), TimeoutMillis: input.TimeoutMillis()}
	case tool.CapabilityRead:
		value, _ := invocation.Read()
		input := value.Input()
		payload.ReadInput = &ReadInputPayload{FilePath: input.FilePath(), Offset: input.Offset(), Limit: input.Limit()}
	case tool.CapabilityGlob:
		value, _ := invocation.Glob()
		input := value.Input()
		payload.GlobInput = &GlobInputPayload{Pattern: input.Pattern(), Path: input.Path(), Limit: input.Limit()}
	case tool.CapabilityGrep:
		value, _ := invocation.Grep()
		input := value.Input()
		payload.GrepInput = &GrepInputPayload{
			Pattern: input.Pattern(), Path: input.Path(), Glob: input.Glob(), OutputMode: input.OutputMode(),
			CaseInsensitive: input.CaseInsensitive(), BeforeContext: input.BeforeContext(), AfterContext: input.AfterContext(), Limit: input.Limit(),
		}
	case tool.CapabilityWrite:
		value, _ := invocation.Write()
		payload.WriteInput = &WriteInputPayload{FilePath: value.Input().FilePath(), Content: value.Input().Content()}
	case tool.CapabilityPatch:
		value, _ := invocation.Patch()
		input := value.Input()
		replacements := input.Replacements()
		encoded := make([]PatchReplacementPayload, len(replacements))
		for index, replacement := range replacements {
			encoded[index] = PatchReplacementPayload{OldText: replacement.OldText(), NewText: replacement.NewText(), ReplaceAll: replacement.ReplaceAll(), LineBased: replacement.LineBased()}
		}
		payload.PatchInput = &PatchInputPayload{FilePath: input.FilePath(), Replacements: encoded}
	}
	if _, err := payload.Domain(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventToolCallReady, "", turnID, payload)
}

// NewToolApprovalDecidedDraft 创建在任何Patch或Exec执行前必须Sync的批准事实。
func NewToolApprovalDecidedDraft(turnID domain.TurnID, payload ToolApprovalDecidedPayload) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := validateToolApprovalDecidedPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventToolApprovalDecided, "", turnID, payload)
}

// NewToolExecutionStartedDraft 创建executor接收前必须单独Sync的事实。
func NewToolExecutionStartedDraft(turnID domain.TurnID, invocationID tool.InvocationID) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	payload := ToolExecutionStartedPayload{InvocationID: invocationID}
	if err := validateToolExecutionStartedPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventToolExecutionStarted, "", turnID, payload)
}

// NewToolCallResultDraft 创建冻结模型输出的工具终态事实。
func NewToolCallResultDraft(turnID domain.TurnID, result tool.InvocationResult) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := result.Validate(); err != nil {
		return RecordDraft{}, fmt.Errorf("tool result is invalid: %w", err)
	}
	payload := ToolCallResultPayload{
		InvocationID: result.InvocationID(), Capability: result.Capability(), WireKind: result.WireKind(), Status: result.Status(), Code: result.Code(), Preview: result.Preview().Text(),
	}
	switch result.Capability() {
	case tool.CapabilityExec:
		m := result.ExecMetadata()
		exit, known := m.ExitCode()
		payload.ExecMetadata = &ExecResultMetadataPayload{Workdir: m.Workdir(), ExitCode: exit, ExitKnown: known, DurationMillis: m.DurationMillis(), StdoutObserved: m.Stdout().ObservedBytes(), StdoutRetained: m.Stdout().RetainedBytes(), StderrObserved: m.Stderr().ObservedBytes(), StderrRetained: m.Stderr().RetainedBytes(), TimedOut: m.TimedOut(), Cancelled: m.Cancelled(), Truncated: m.OutputTruncated()}
	case tool.CapabilityRead:
		metadata := result.ReadMetadata()
		payload.ReadMetadata = &ReadResultMetadataPayload{
			RelativePath: metadata.RelativePath(), RequestedOffset: metadata.RequestedOffset(),
			RequestedLimit: metadata.RequestedLimit(), StartLine: metadata.StartLine(), EndLine: metadata.EndLine(),
			ReachedEOF: metadata.ReachedEOF(), LongLineTruncated: metadata.LongLineTruncated(),
			OutputTruncated: metadata.OutputTruncated(),
		}
	case tool.CapabilityGlob:
		metadata := result.GlobMetadata()
		payload.GlobMetadata = encodeGlobMetadata(metadata)
	case tool.CapabilityGrep:
		metadata := result.GrepMetadata()
		payload.GrepMetadata = encodeGrepMetadata(metadata)
	case tool.CapabilityWrite:
		m := result.WriteMetadata()
		payload.WriteMetadata = &WriteResultMetadataPayload{RelativePath: m.RelativePath(), Operation: m.Operation(), Bytes: m.Bytes(), Lines: m.Lines(), CreatedDirectories: m.CreatedDirectories()}
	case tool.CapabilityPatch:
		metadata := result.PatchMetadata()
		payload.PatchMetadata = &PatchResultMetadataPayload{RelativePath: metadata.RelativePath(), ReplacementCount: metadata.ReplacementCount(), AddedLines: metadata.AddedLines(), RemovedLines: metadata.RemovedLines()}
	}
	if err := validateToolCallResultPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventToolCallResult, "", turnID, payload)
}

// NewTurnCompletedDraft 创建已验证并编码的当前 turn_completed draft。
func NewTurnCompletedDraft(turnID domain.TurnID) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnCompleted, "", turnID, TurnCompletedPayload{})
}

// NewTurnFailedDraft 创建已验证并编码的当前 turn_failed draft。
func NewTurnFailedDraft(turnID domain.TurnID, payload TurnFailedPayload) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := validateTurnFailedPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnFailed, "", turnID, payload)
}

func encodeDraft[T recordPayload](
	kind EventKind,
	parentThreadID domain.ThreadID,
	turnID domain.TurnID,
	payload T,
) (RecordDraft, error) {
	descriptor, exists := descriptorByKind(kind)
	if !exists {
		return RecordDraft{}, fmt.Errorf("unsupported session event kind")
	}
	encoded, err := codec.MarshalStable(payload)
	if err != nil {
		return RecordDraft{}, fmt.Errorf("marshal session payload: %w", err)
	}
	if len(encoded) == 0 || len(encoded) > MaxRecordBytes {
		return RecordDraft{}, fmt.Errorf("session payload exceeds size limit")
	}
	return RecordDraft{
		descriptor: descriptor, parentThreadID: parentThreadID, turnID: turnID,
		payload: append(json.RawMessage(nil), encoded...),
	}, nil
}

func (draft RecordDraft) validate() error {
	descriptor, exists := descriptorByKind(draft.descriptor.Kind)
	if !exists || descriptor != draft.descriptor {
		return fmt.Errorf("session draft descriptor is invalid")
	}
	if len(draft.payload) == 0 || len(draft.payload) > MaxRecordBytes || !codec.Valid(draft.payload) {
		return fmt.Errorf("session draft payload is invalid")
	}
	if err := validateDraftPlacement(draft); err != nil {
		return err
	}
	return validateDraftPayload(draft)
}

func (draft RecordDraft) clone() RecordDraft {
	draft.payload = draft.PayloadBytes()
	return draft
}

func validateDraftPlacement(draft RecordDraft) error {
	switch draft.descriptor.Kind {
	case EventSessionMeta:
		if draft.parentThreadID != "" || draft.turnID != "" {
			return fmt.Errorf("session metadata draft placement is invalid")
		}
	case EventThreadMeta:
		if draft.turnID != "" {
			return fmt.Errorf("thread metadata draft placement is invalid")
		}
	case EventTurnStarted, EventProviderNativeCommit, EventSampleUsage, EventToolCallReady,
		EventToolApprovalDecided, EventToolExecutionStarted, EventToolCallResult, EventTurnCompleted, EventTurnFailed:
		if draft.parentThreadID != "" || !draft.turnID.Valid() {
			return fmt.Errorf("turn draft placement is invalid")
		}
	default:
		return fmt.Errorf("session draft event kind is invalid")
	}
	return nil
}

func validateDraftPayload(draft RecordDraft) error {
	record := Record{
		PayloadVersion: EnvelopeVersion,
		EventKind:      draft.descriptor.Kind, Payload: draft.PayloadBytes(),
	}
	switch draft.descriptor.Kind {
	case EventSessionMeta:
		_, err := DecodeSessionMetaPayload(record)
		return err
	case EventThreadMeta:
		payload, err := DecodeThreadMetaPayload(record)
		if err == nil && payload.ParentThreadID != draft.parentThreadID {
			return fmt.Errorf("thread metadata parent does not match draft")
		}
		return err
	case EventTurnStarted:
		_, err := DecodeTurnStartedPayload(record)
		return err
	case EventProviderNativeCommit:
		_, err := DecodeNativeCommitPayload(record)
		return err
	case EventSampleUsage:
		_, err := DecodeSampleUsagePayload(record)
		return err
	case EventToolCallReady:
		_, err := DecodeToolCallReadyPayload(record)
		return err
	case EventToolExecutionStarted:
		_, err := DecodeToolExecutionStartedPayload(record)
		return err
	case EventToolApprovalDecided:
		_, err := DecodeToolApprovalDecidedPayload(record)
		return err
	case EventToolCallResult:
		_, err := DecodeToolCallResultPayload(record)
		return err
	case EventTurnCompleted:
		_, err := DecodeTurnCompletedPayload(record)
		return err
	case EventTurnFailed:
		_, err := DecodeTurnFailedPayload(record)
		return err
	default:
		return fmt.Errorf("session draft event kind is invalid")
	}
}

func validateTurnID(turnID domain.TurnID) error {
	if !turnID.Valid() {
		return fmt.Errorf("session turn ID is invalid")
	}
	return nil
}
