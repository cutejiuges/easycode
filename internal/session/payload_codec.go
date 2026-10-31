package session

import (
	"easycode/internal/codec"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"easycode/internal/tool"
)

// DecodeSessionMetaPayload 严格解码并验证 session_meta 当前 payload。
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

// DecodeThreadMetaPayload 严格解码并验证 thread_meta 当前 payload。
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

// DecodeTurnStartedPayload 严格解码并验证 turn_started 当前 payload。
func DecodeTurnStartedPayload(record Record) (TurnStartedPayload, error) {
	return decodeKnownPayload[TurnStartedPayload](record, EventTurnStarted)
}

// DecodeNativeCommitPayload 严格解码并验证 provider_native_commit 当前 payload。
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

// DecodeSampleUsagePayload 严格解码并验证 sample_usage 当前 payload。
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
	if payload.Capability != tool.CapabilityReadArtifact && payload.ReadArtifactInput != nil {
		return tool.Invocation{}, fmt.Errorf("unexpected artifact input")
	}
	if payload.Capability != tool.CapabilityExec && payload.ExecInput != nil {
		return tool.Invocation{}, fmt.Errorf("unexpected Exec input")
	}
	if !payload.InvocationID.Valid() || !payload.ProviderCallID.Valid() || !payload.WireKind.Valid() || payload.SampleIndex >= 16 || payload.CallIndex >= 64 {
		return tool.Invocation{}, fmt.Errorf("tool call ready payload is invalid")
	}
	if payload.Capability != tool.CapabilityWrite && payload.WriteInput != nil {
		return tool.Invocation{}, fmt.Errorf("unexpected Write input")
	}
	var ready tool.ReadyCall
	var err error
	switch payload.Capability {
	case tool.CapabilityReadArtifact:
		if payload.WireKind != tool.WireStructured || payload.ReadArtifactInput == nil || payload.ReadInput != nil || payload.GlobInput != nil || payload.GrepInput != nil || payload.PatchInput != nil || payload.WriteInput != nil || payload.ExecInput != nil {
			return tool.Invocation{}, fmt.Errorf("invalid artifact input union")
		}
		w := payload.ReadArtifactInput
		input, e := tool.NewReadArtifactInput(w.ID, w.Offset, w.Limit)
		if e != nil {
			return tool.Invocation{}, e
		}
		ready, err = tool.NewReadArtifactReadyCall(payload.ProviderCallID, input)

	case tool.CapabilityExec:
		if payload.WireKind != tool.WireStructured || payload.ExecInput == nil || payload.ReadInput != nil || payload.GlobInput != nil || payload.GrepInput != nil || payload.PatchInput != nil || payload.ExecInput.TimeoutMillis < tool.MinExecTimeoutMillis {
			return tool.Invocation{}, fmt.Errorf("invalid Exec ready payload")
		}
		input, inputErr := tool.NewExecInput(payload.ExecInput.Command, payload.ExecInput.Workdir, payload.ExecInput.TimeoutMillis)
		if inputErr != nil {
			return tool.Invocation{}, inputErr
		}
		ready, err = tool.NewExecReadyCall(payload.ProviderCallID, input)
	case tool.CapabilityRead:
		if payload.WireKind != tool.WireStructured || payload.ReadInput == nil || payload.GlobInput != nil || payload.GrepInput != nil || payload.PatchInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Read capability")
		}
		input, inputErr := tool.NewReadInput(payload.ReadInput.FilePath, payload.ReadInput.Offset, payload.ReadInput.Limit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Read input is invalid: %w", inputErr)
		}
		ready, err = tool.NewReadReadyCall(payload.ProviderCallID, input)
	case tool.CapabilityGlob:
		if payload.WireKind != tool.WireStructured || payload.GlobInput == nil || payload.ReadInput != nil || payload.GrepInput != nil || payload.PatchInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Glob capability")
		}
		input, inputErr := tool.NewGlobInput(payload.GlobInput.Pattern, payload.GlobInput.Path, payload.GlobInput.Limit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Glob input is invalid: %w", inputErr)
		}
		ready, err = tool.NewGlobReadyCall(payload.ProviderCallID, input)
	case tool.CapabilityGrep:
		if payload.WireKind != tool.WireStructured || payload.GrepInput == nil || payload.ReadInput != nil || payload.GlobInput != nil || payload.PatchInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Grep capability")
		}
		value := payload.GrepInput
		input, inputErr := tool.NewGrepInput(value.Pattern, value.Path, value.Glob, value.OutputMode, value.CaseInsensitive, value.BeforeContext, value.AfterContext, value.Limit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Grep input is invalid: %w", inputErr)
		}
		ready, err = tool.NewGrepReadyCall(payload.ProviderCallID, input)
	case tool.CapabilityPatch:
		if payload.PatchInput == nil || payload.ReadInput != nil || payload.GlobInput != nil || payload.GrepInput != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready payload does not match Patch capability")
		}
		replacements := make([]tool.PatchReplacement, len(payload.PatchInput.Replacements))
		for index, value := range payload.PatchInput.Replacements {
			var replacement tool.PatchReplacement
			var inputErr error
			if value.LineBased {
				oldLines := strings.Split(strings.TrimSuffix(value.OldText, "\n"), "\n")
				var newLines []string
				if value.NewText != "" {
					newLines = strings.Split(strings.TrimSuffix(value.NewText, "\n"), "\n")
				}
				replacement, inputErr = tool.NewPatchLineReplacement(oldLines, newLines)
				if value.ReplaceAll {
					inputErr = fmt.Errorf("line replacement cannot replace all")
				}
			} else {
				replacement, inputErr = tool.NewPatchReplacement(value.OldText, value.NewText, value.ReplaceAll)
			}
			if inputErr != nil {
				return tool.Invocation{}, fmt.Errorf("tool call ready Patch replacement is invalid")
			}
			replacements[index] = replacement
		}
		input, inputErr := tool.NewPatchInput(payload.PatchInput.FilePath, replacements)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call ready Patch input is invalid")
		}
		ready, err = tool.NewPatchReadyCall(payload.ProviderCallID, payload.WireKind, input)
	case tool.CapabilityWrite:
		if payload.WireKind != tool.WireStructured || payload.WriteInput == nil || payload.ReadInput != nil || payload.GlobInput != nil || payload.GrepInput != nil || payload.PatchInput != nil {
			return tool.Invocation{}, fmt.Errorf("invalid Write input union")
		}
		input, inputErr := tool.NewWriteInput(payload.WriteInput.FilePath, payload.WriteInput.Content)
		if inputErr != nil {
			return tool.Invocation{}, inputErr
		}
		ready, err = tool.NewWriteReadyCall(payload.ProviderCallID, input)
	default:
		return tool.Invocation{}, fmt.Errorf("tool call ready capability is invalid")
	}
	if err != nil {
		return tool.Invocation{}, fmt.Errorf("tool call ready identity is invalid: %w", err)
	}
	return tool.NewInvocation(payload.InvocationID, ready)
}

// DecodeToolApprovalDecidedPayload 严格解码并验证批准事实。
func DecodeToolApprovalDecidedPayload(record Record) (ToolApprovalDecidedPayload, error) {
	payload, err := decodeKnownPayload[ToolApprovalDecidedPayload](record, EventToolApprovalDecided)
	if err != nil {
		return ToolApprovalDecidedPayload{}, err
	}
	if err := validateToolApprovalDecidedPayload(payload); err != nil {
		return ToolApprovalDecidedPayload{}, err
	}
	return payload, nil
}
func validateToolApprovalDecidedPayload(payload ToolApprovalDecidedPayload) error {
	if !payload.Source.Valid() || payload.Metadata.Validate() != nil ||
		(payload.Source == ApprovalSourceHeadless && payload.Outcome != tool.ApprovalOutcomeDeny) ||
		(payload.Source == ApprovalSourceInterruption && payload.Outcome != tool.ApprovalOutcomeAbort) ||
		(payload.Source == ApprovalSourceSessionGrant && payload.Outcome != tool.ApprovalOutcomeAllowFromSession) {
		return fmt.Errorf("tool approval source or metadata is invalid")
	}
	if !payload.RequestID.Valid() || !payload.InvocationID.Valid() || !payload.Fingerprint.Valid() || !payload.Outcome.Valid() || len(payload.Feedback) > tool.MaxApprovalFeedbackBytes || !utf8.ValidString(payload.Feedback) || (payload.Outcome != tool.ApprovalOutcomeDeny && payload.Feedback != "") {
		return fmt.Errorf("tool approval decided payload is invalid")
	}
	return nil
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
	if payload.ExecMetadata != nil {
		for _, s := range []tool.ArtifactSaving{payload.ExecMetadata.StdoutSaving, payload.ExecMetadata.StderrSaving} {
			if d, ok := s.Descriptor(); ok && record.ThreadID != "" && d.ThreadID() != record.ThreadID {
				return ToolCallResultPayload{}, fmt.Errorf("artifact thread does not match result")
			}
		}
	}
	for _, metadata := range []*tool.ArtifactSaving{searchPayloadSaving(payload)} {
		if metadata != nil {
			if d, ok := metadata.Descriptor(); ok && record.ThreadID != "" && d.ThreadID() != record.ThreadID {
				return ToolCallResultPayload{}, fmt.Errorf("artifact thread does not match result")
			}
		}
	}
	return payload, nil
}

func searchPayloadSaving(payload ToolCallResultPayload) *tool.ArtifactSaving {
	if payload.GlobMetadata != nil {
		return &payload.GlobMetadata.Saving
	}
	if payload.GrepMetadata != nil {
		return &payload.GrepMetadata.Saving
	}
	return nil
}

func validateToolCallResultPayload(payload ToolCallResultPayload) error {
	if payload.Budget.Validate() != nil || !payload.InvocationID.Valid() || !payload.Capability.Valid() || !payload.WireKind.Valid() || !payload.Status.Valid() || payload.Code == "" || !utf8.ValidString(payload.Preview) {
		return fmt.Errorf("tool call result payload is invalid")
	}
	invocation, err := validationInvocation(payload)
	if err != nil {
		return err
	}
	_, err = decodeResultPayload(payload, invocation)
	return err
}

// Domain 使用对应ready invocation重建冻结工具结果。
func (payload ToolCallResultPayload) Domain(invocation tool.Invocation) (tool.InvocationResult, error) {
	result, err := decodeResultPayload(payload, invocation)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	if value, ok := invocation.Write(); ok && result.Status() == tool.ResultSuccess {
		m := result.WriteMetadata()
		if m.Bytes() != len(value.Input().Content()) || m.Lines() != tool.WriteLineCount(value.Input().Content()) {
			return tool.InvocationResult{}, fmt.Errorf("write result counts do not match input")
		}
	}
	return result, nil
}

func decodeResultPayload(payload ToolCallResultPayload, invocation tool.Invocation) (tool.InvocationResult, error) {
	if payload.Capability != tool.CapabilityReadArtifact && payload.ReadArtifactMetadata != nil {
		return tool.InvocationResult{}, fmt.Errorf("unexpected artifact metadata")
	}
	if payload.WireKind != invocation.WireKind() || (payload.Capability != tool.CapabilityWrite && payload.WriteMetadata != nil) || (payload.Capability != tool.CapabilityExec && payload.ExecMetadata != nil) {
		return tool.InvocationResult{}, fmt.Errorf("result wire or metadata mismatch")
	}
	if err := invocation.Validate(); err != nil || invocation.InvocationID() != payload.InvocationID || invocation.Capability() != payload.Capability {
		return tool.InvocationResult{}, fmt.Errorf("tool call result invocation is invalid")
	}
	preview, err := tool.NewModelPreview(payload.Preview)
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result preview is invalid: %w", err)
	}
	var result tool.InvocationResult
	switch payload.Capability {
	case tool.CapabilityReadArtifact:
		if payload.WireKind != tool.WireStructured || payload.ReadArtifactMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil || payload.PatchMetadata != nil || payload.WriteMetadata != nil || payload.ExecMetadata != nil {
			return tool.InvocationResult{}, fmt.Errorf("invalid artifact result union")
		}
		m := payload.ReadArtifactMetadata
		v, _ := invocation.ReadArtifact()
		input, e := tool.NewReadArtifactInput(m.Input.ID, m.Input.Offset, m.Input.Limit)
		if e != nil || input != v.Input() {
			return tool.InvocationResult{}, fmt.Errorf("artifact result range mismatch")
		}
		var page tool.ArtifactPage
		if m.Page != nil {
			page = *m.Page
		}
		result, err = tool.NewReadArtifactInvocationResult(v, payload.Status, payload.Code, preview, page)

	case tool.CapabilityExec:
		if payload.WireKind != tool.WireStructured || payload.ExecMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil || payload.PatchMetadata != nil {
			return tool.InvocationResult{}, fmt.Errorf("invalid Exec result union")
		}
		m := payload.ExecMetadata
		out, outErr := tool.NewExecStreamStats(m.StdoutObserved, m.StdoutRetained)
		stderr, errErr := tool.NewExecStreamStats(m.StderrObserved, m.StderrRetained)
		if outErr != nil || errErr != nil {
			return tool.InvocationResult{}, fmt.Errorf("invalid Exec output statistics")
		}
		metadata, metadataErr := tool.NewExecResultMetadata(m.Workdir, m.ExitCode, m.ExitKnown, m.DurationMillis, out, stderr, m.TimedOut, m.Cancelled, m.Truncated)
		if metadataErr != nil {
			return tool.InvocationResult{}, metadataErr
		}
		metadata, metadataErr = metadata.WithOutputSaving(m.StdoutSaving, m.StderrSaving)
		if metadataErr != nil {
			return tool.InvocationResult{}, metadataErr
		}
		value, _ := invocation.Exec()
		result, err = tool.NewExecInvocationResult(value, payload.Status, payload.Code, preview, metadata)
	case tool.CapabilityRead:
		result, err = decodeReadResult(payload, invocation, preview)
	case tool.CapabilityGlob:
		result, err = decodeGlobResult(payload, invocation, preview)
	case tool.CapabilityGrep:
		result, err = decodeGrepResult(payload, invocation, preview)
	case tool.CapabilityWrite:
		result, err = decodeWriteResult(payload, invocation, preview)
	case tool.CapabilityPatch:
		if payload.PatchMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil {
			return tool.InvocationResult{}, fmt.Errorf("patch metadata union is invalid")
		}
		metadata, metadataErr := tool.NewPatchResultMetadata(payload.PatchMetadata.RelativePath, payload.PatchMetadata.ReplacementCount, payload.PatchMetadata.AddedLines, payload.PatchMetadata.RemovedLines)
		if metadataErr != nil {
			return tool.InvocationResult{}, metadataErr
		}
		value, ok := invocation.Patch()
		if !ok {
			return tool.InvocationResult{}, fmt.Errorf("patch invocation is required")
		}
		result, err = tool.NewPatchInvocationResult(value, payload.Status, payload.Code, preview, metadata)
	default:
		return tool.InvocationResult{}, fmt.Errorf("tool call result capability is invalid")
	}
	if err != nil {
		return tool.InvocationResult{}, fmt.Errorf("tool call result payload is invalid: %w", err)
	}
	result, err = result.WithBudget(payload.Budget)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return result, tool.ValidateFrozenResult(result)
}

func validationInvocation(payload ToolCallResultPayload) (tool.Invocation, error) {
	callID, _ := tool.ParseProviderCallID("payload-validation")
	if payload.Capability != tool.CapabilityWrite && payload.WriteMetadata != nil {
		return tool.Invocation{}, fmt.Errorf("unexpected Write metadata")
	}
	var ready tool.ReadyCall
	var err error
	switch payload.Capability {
	case tool.CapabilityReadArtifact:
		if payload.ReadArtifactMetadata == nil {
			return tool.Invocation{}, fmt.Errorf("missing artifact metadata")
		}
		m := payload.ReadArtifactMetadata.Input
		input, e := tool.NewReadArtifactInput(m.ID, m.Offset, m.Limit)
		if e != nil {
			return tool.Invocation{}, e
		}
		ready, err = tool.NewReadArtifactReadyCall(callID, input)

	case tool.CapabilityExec:
		if payload.ExecMetadata == nil {
			return tool.Invocation{}, fmt.Errorf("missing Exec metadata")
		}
		input, _ := tool.NewExecInput("validation", payload.ExecMetadata.Workdir, tool.DefaultExecTimeoutMillis)
		ready, err = tool.NewExecReadyCall(callID, input)
	case tool.CapabilityRead:
		if payload.WireKind != tool.WireStructured || payload.ReadMetadata == nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil || payload.PatchMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Read capability")
		}
		input, inputErr := tool.NewReadInput(".", payload.ReadMetadata.RequestedOffset, payload.ReadMetadata.RequestedLimit)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result Read metadata is invalid: %w", inputErr)
		}
		ready, err = tool.NewReadReadyCall(callID, input)
	case tool.CapabilityGlob:
		if payload.WireKind != tool.WireStructured || payload.GlobMetadata == nil || payload.ReadMetadata != nil || payload.GrepMetadata != nil || payload.PatchMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Glob capability")
		}
		input, _ := tool.NewGlobInput("**", "", 1)
		ready, err = tool.NewGlobReadyCall(callID, input)
	case tool.CapabilityGrep:
		if payload.WireKind != tool.WireStructured || payload.GrepMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.PatchMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Grep capability")
		}
		input, inputErr := tool.NewGrepInput("x", "", "", payload.GrepMetadata.Mode, false, 0, 0, 1)
		if inputErr != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result Grep metadata is invalid: %w", inputErr)
		}
		ready, err = tool.NewGrepReadyCall(callID, input)
	case tool.CapabilityWrite:
		if payload.WriteMetadata == nil || payload.WireKind != tool.WireStructured || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil || payload.PatchMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("invalid Write metadata union")
		}
		input, inputErr := tool.NewWriteInput(payload.WriteMetadata.RelativePath, "")
		if inputErr != nil {
			return tool.Invocation{}, inputErr
		}
		ready, err = tool.NewWriteReadyCall(callID, input)
	case tool.CapabilityPatch:
		if payload.PatchMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil {
			return tool.Invocation{}, fmt.Errorf("tool call result metadata does not match Patch capability")
		}
		replacement, _ := tool.NewPatchReplacement("validation-old", "validation-new", false)
		input, _ := tool.NewPatchInput(payload.PatchMetadata.RelativePath, []tool.PatchReplacement{replacement})
		ready, err = tool.NewPatchReadyCall(callID, payload.WireKind, input)
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
	metadata, err = metadata.WithModelOmissions(metadataPayload.ModelOmittedMatches)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	result, err := tool.NewGlobInvocationResult(value, payload.Status, payload.Code, preview, metadata)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return result.WithSearchSaving(metadataPayload.Saving)
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
	metadata, err = metadata.WithModelOmissions(metadataPayload.ModelOmittedMatches)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	result, err := tool.NewGrepInvocationResult(value, payload.Status, payload.Code, preview, metadata)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return result.WithSearchSaving(metadataPayload.Saving)
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
	return &GlobResultMetadataPayload{ModelOmittedMatches: metadata.ModelOmittedMatches(),
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
	return &GrepResultMetadataPayload{ModelOmittedMatches: metadata.ModelOmittedMatches(),
		Mode: metadata.Mode(), Matches: payloadMatches, MatchingLines: metadata.MatchingLines(),
		Truncated: metadata.Truncated(), OmittedMatches: metadata.OmittedMatches(), VisitedEntries: metadata.VisitedEntries(),
		ScannedFiles: metadata.ScannedFiles(), ScannedBytes: metadata.ScannedBytes(),
		IncompleteReason: metadata.IncompleteReason(), Skipped: encodeSkipCounts(metadata.Skipped()),
	}
}

// DecodeTurnCompletedPayload 严格解码并验证 turn_completed 当前 payload。
func DecodeTurnCompletedPayload(record Record) (TurnCompletedPayload, error) {
	return decodeKnownPayload[TurnCompletedPayload](record, EventTurnCompleted)
}

// DecodeTurnFailedPayload 严格解码并验证 turn_failed 当前 payload。
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
	if err := codec.UnmarshalRecord(record.Payload, &zero); err != nil {
		return zero, fmt.Errorf("decode session payload: %w", err)
	}
	return zero, nil
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
		len(payload.Payload) == 0 || len(payload.Payload) > MaxRecordBytes || !codec.Valid(payload.Payload) {
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

func decodeKnownRecord(record Record) error {
	switch record.EventKind {
	case EventCompactionStarted:
		_, err := DecodeCompactionStartedPayload(record)
		return err
	case EventCompactionCompleted:
		_, err := DecodeCompactionCompletedPayload(record)
		return err
	case EventCompactionFailed:
		_, err := DecodeCompactionFailedPayload(record)
		return err

	case EventTurnInputsCommitted:
		_, err := DecodeTurnInputsPayload(record)
		return err
	case EventSessionMeta:
		_, err := DecodeSessionMetaPayload(record)
		return err
	case EventThreadMeta:
		_, err := DecodeThreadMetaPayload(record)
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
		return fmt.Errorf("session event kind is unsupported")
	}
}
