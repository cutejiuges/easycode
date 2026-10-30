package openai

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

const (
	responsesWire               = "responses"
	nativeCommitPayloadRevision = 2
)

type nativeHistoryCommit struct {
	Kind          nativeHistoryEntryKind  `json:"kind"`
	UserInputs    []NativeItem            `json:"user_inputs,omitempty"`
	InputMetadata *provider.InputMetadata `json:"input_metadata,omitempty"`
	Outputs       []NativeItem            `json:"outputs,omitempty"`
	ToolOutputs   []NativeItem            `json:"tool_outputs,omitempty"`
	Usage         *rawUsageCommit         `json:"usage,omitempty"`
}

type optionalUintCommit struct {
	Known bool    `json:"known"`
	Value *uint64 `json:"value,omitempty"`
}

type rawUsageCommit struct {
	InputTokens           optionalUintCommit `json:"input_tokens"`
	CachedInputTokens     optionalUintCommit `json:"cached_input_tokens"`
	CacheWriteTokens      optionalUintCommit `json:"cache_write_tokens"`
	OutputTokens          optionalUintCommit `json:"output_tokens"`
	ReasoningOutputTokens optionalUintCommit `json:"reasoning_output_tokens"`
}

func encodeNativeCommit(entry nativeHistoryEntry) (provider.NativeCommitEnvelope, error) {
	cloned := entry.clone()
	if err := validateNativeHistoryEntry(cloned); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	commit := nativeHistoryCommit{
		Kind: cloned.Kind, UserInputs: cloned.UserInputs, InputMetadata: cloned.InputMetadata, Outputs: cloned.Outputs,
		ToolOutputs: cloned.ToolOutputs,
	}
	if cloned.Kind == nativeHistorySample {
		commit.Usage = encodeRawUsage(cloned.Usage)
	}
	payload, err := codec.MarshalStable(commit)
	if err != nil {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("encode OpenAI native commit: %w", err)
	}
	if len(payload) > provider.MaxNativeCommitBytes {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("OpenAI native commit exceeds size limit")
	}
	return provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, responsesWire, nativeCommitPayloadRevision, payload,
	)
}

func decodeNativeCommit(envelope provider.NativeCommitEnvelope) (nativeHistoryEntry, error) {
	if envelope.Family() != domain.ProviderOpenAI || envelope.Wire() != responsesWire ||
		envelope.PayloadVersion() != nativeCommitPayloadRevision {
		return nativeHistoryEntry{}, fmt.Errorf("OpenAI native commit boundary is incompatible")
	}
	payload := envelope.Payload()
	if len(payload) == 0 || len(payload) > provider.MaxNativeCommitBytes {
		return nativeHistoryEntry{}, fmt.Errorf("OpenAI native commit size is invalid")
	}
	var commit nativeHistoryCommit
	if err := codec.UnmarshalRecord(payload, &commit); err != nil {
		return nativeHistoryEntry{}, fmt.Errorf("decode native commit: %w", err)
	}
	entry := nativeHistoryEntry{
		Kind: commit.Kind, Outputs: cloneNativeItems(commit.Outputs),
		ToolOutputs: cloneNativeItems(commit.ToolOutputs),
	}
	entry.UserInputs = commit.UserInputs
	entry.InputMetadata = commit.InputMetadata
	if commit.InputMetadata != nil {
		entry.TurnID = commit.InputMetadata.TurnID
	}

	if commit.Kind == nativeHistorySample {
		usage, err := decodeRawUsage(commit.Usage)
		if err != nil {
			return nativeHistoryEntry{}, err
		}
		entry.Usage = usage
	} else if commit.Usage != nil {
		return nativeHistoryEntry{}, fmt.Errorf("OpenAI tool outputs must not contain usage")
	}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry.clone(), nil
}

func validateNativeHistoryEntry(entry nativeHistoryEntry) error {
	switch entry.Kind {
	case nativeHistoryUserInputs:
		if entry.InputMetadata == nil || len(entry.UserInputs) == 0 || len(entry.Outputs) != 0 || len(entry.ToolOutputs) != 0 {
			return fmt.Errorf("user input entry fields are invalid")
		}
		a, err := entry.InputMetadata.Association()
		if err != nil {
			return err
		}
		if len(a.IDs()) != len(entry.UserInputs) {
			return fmt.Errorf("user input count does not match metadata")
		}
		for _, input := range entry.UserInputs {
			if err := validateUserItem(input); err != nil {
				return err
			}
		}

	case nativeHistorySample:
		if len(entry.Outputs) == 0 || len(entry.ToolOutputs) != 0 {
			return fmt.Errorf("OpenAI sample entry fields are invalid")
		}
		if len(entry.UserInputs) != 0 || entry.InputMetadata != nil {
			return fmt.Errorf("sample contains user input fields")
		}

		seenCallIDs := make(map[string]struct{})
		for _, item := range entry.Outputs {
			if err := validateOutputItem(item); err != nil {
				return err
			}
			if item.Type == "function_call" || item.Type == "custom_tool_call" {
				if _, exists := seenCallIDs[item.CallID]; exists {
					return fmt.Errorf("OpenAI function call ID is duplicated")
				}
				seenCallIDs[item.CallID] = struct{}{}
			}
		}
		if _, err := entry.Usage.normalized(); err != nil {
			return fmt.Errorf("OpenAI native commit usage is invalid: %w", err)
		}
	case nativeHistoryToolOutputs:
		if len(entry.UserInputs) != 0 || entry.InputMetadata != nil || len(entry.Outputs) != 0 || len(entry.ToolOutputs) == 0 {
			return fmt.Errorf("OpenAI tool outputs entry fields are invalid")
		}
		for _, item := range entry.ToolOutputs {
			if (item.Type != "function_call_output" && item.Type != "custom_tool_call_output") || item.CallID == "" || !codec.Valid([]byte(item.Output)) {
				return fmt.Errorf("OpenAI tool call output item is invalid")
			}
		}
	default:
		return fmt.Errorf("OpenAI native commit kind is unsupported")
	}
	return nil
}

func validateUserItem(item NativeItem) error {
	// 当前用户输入只有一个纯文本片段；原生 opaque 扩展仅允许模型输出持有。
	encoded, err := codec.MarshalStable(item)
	if err != nil {
		return fmt.Errorf("user input encoding is invalid")
	}
	var current struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := codec.UnmarshalRecord(encoded, &current); err != nil {
		return fmt.Errorf("user input shape is invalid")
	}
	if current.Role != "user" || current.Type != "message" || len(current.Content) != 1 || current.Content[0].Type != "input_text" {
		return fmt.Errorf("user input content is invalid")
	}
	return domain.ValidateUserText(current.Content[0].Text)
}

func validateOutputItem(item NativeItem) error {
	if item.Type == "" {
		return fmt.Errorf("OpenAI native commit output item type is required")
	}
	if len(item.Raw) > 0 && !codec.Valid(item.Raw) {
		return fmt.Errorf("OpenAI native commit output item raw JSON is invalid")
	}
	if item.Type == "message" && item.Role != "assistant" {
		return fmt.Errorf("OpenAI native commit message role is invalid")
	}
	if item.Type == "function_call" {
		if item.Name == "Write" {
			if _, err := tool.DecodeWriteInput([]byte(item.Arguments)); err != nil {
				return fmt.Errorf("invalid native Write input")
			}
		}
		if item.CallID == "" || item.Name == "" || item.Arguments == "" || !codec.Valid([]byte(item.Arguments)) {
			return fmt.Errorf("OpenAI function call item is invalid")
		}
	}
	if item.Type == "custom_tool_call" {
		if item.ID == "" || item.CallID == "" || item.Name != "apply_patch" || item.Input == "" || len(item.Input) > tool.MaxPatchInputBytes {
			return fmt.Errorf("OpenAI custom tool call item is invalid")
		}
		if _, err := tool.ParseOpenAIApplyPatchInput([]byte(item.Input)); err != nil {
			return fmt.Errorf("OpenAI custom tool call input is invalid")
		}
	}
	return nil
}

func encodeRawUsage(usage rawUsage) *rawUsageCommit {
	return &rawUsageCommit{
		InputTokens:           encodeOptionalUint(usage.InputTokens),
		CachedInputTokens:     encodeOptionalUint(usage.CachedInputTokens),
		CacheWriteTokens:      encodeOptionalUint(usage.CacheWriteTokens),
		OutputTokens:          encodeOptionalUint(usage.OutputTokens),
		ReasoningOutputTokens: encodeOptionalUint(usage.ReasoningOutputTokens),
	}
}

func decodeRawUsage(commit *rawUsageCommit) (rawUsage, error) {
	if commit == nil {
		return rawUsage{}, fmt.Errorf("OpenAI native commit usage is required")
	}
	fields := [...]optionalUintCommit{
		commit.InputTokens, commit.CachedInputTokens, commit.CacheWriteTokens,
		commit.OutputTokens, commit.ReasoningOutputTokens,
	}
	for _, field := range fields {
		if field.Known != (field.Value != nil) {
			return rawUsage{}, fmt.Errorf("OpenAI native commit usage state is invalid")
		}
	}
	usage := rawUsage{
		InputTokens:           decodeOptionalUint(commit.InputTokens),
		CachedInputTokens:     decodeOptionalUint(commit.CachedInputTokens),
		CacheWriteTokens:      decodeOptionalUint(commit.CacheWriteTokens),
		OutputTokens:          decodeOptionalUint(commit.OutputTokens),
		ReasoningOutputTokens: decodeOptionalUint(commit.ReasoningOutputTokens),
	}
	if _, err := usage.normalized(); err != nil {
		return rawUsage{}, fmt.Errorf("OpenAI native commit usage is invalid: %w", err)
	}
	return usage, nil
}

func encodeOptionalUint(value optionalUint) optionalUintCommit {
	encoded := optionalUintCommit{Known: value.Known}
	if value.Known {
		copy := value.Value
		encoded.Value = &copy
	}
	return encoded
}

func decodeOptionalUint(value optionalUintCommit) optionalUint {
	if !value.Known || value.Value == nil {
		return optionalUint{Known: value.Known}
	}
	return optionalUint{Known: true, Value: *value.Value}
}

func cloneNativeItems(items []NativeItem) []NativeItem {
	cloned := make([]NativeItem, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, item.clone())
	}
	return cloned
}
