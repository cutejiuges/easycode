package anthropic

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

const (
	messagesWire                = "messages"
	nativeCommitPayloadRevision = 2
)

type nativeHistoryCommit struct {
	Kind          nativeHistoryEntryKind  `json:"kind"`
	UserInputs    []nativeMessage         `json:"user_inputs,omitempty"`
	InputMetadata *provider.InputMetadata `json:"input_metadata,omitempty"`
	Assistant     *nativeMessage          `json:"assistant,omitempty"`
	ToolOutputs   *nativeMessage          `json:"tool_outputs,omitempty"`
	Metadata      *messageMetadataCommit  `json:"metadata,omitempty"`
}

type messageMetadataCommit struct {
	ID         string               `json:"id"`
	Model      string               `json:"model"`
	StopReason optionalStringCommit `json:"stop_reason"`
	Usage      rawUsageCommit       `json:"usage"`
}

type optionalStringCommit struct {
	Known bool   `json:"known"`
	Value string `json:"value,omitempty"`
}

type optionalUintCommit struct {
	Known bool    `json:"known"`
	Value *uint64 `json:"value,omitempty"`
}

type rawUsageCommit struct {
	InputTokens              optionalUintCommit `json:"input_tokens"`
	CacheCreationInputTokens optionalUintCommit `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     optionalUintCommit `json:"cache_read_input_tokens"`
	OutputTokens             optionalUintCommit `json:"output_tokens"`
}

func encodeNativeCommit(entry nativeHistoryEntry) (provider.NativeCommitEnvelope, error) {
	cloned := entry.clone()
	if err := validateNativeHistoryEntry(cloned); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	commit := nativeHistoryCommit{Kind: cloned.Kind, UserInputs: cloned.UserInputs, InputMetadata: cloned.InputMetadata}
	if cloned.Kind == nativeHistorySample {
		assistant := cloned.Assistant.clone()
		metadata := encodeMetadata(cloned.Metadata)
		commit.Assistant = &assistant
		commit.Metadata = &metadata
	} else if cloned.Kind == nativeHistoryToolOutputs {
		outputs := cloned.ToolOutputs.clone()
		commit.ToolOutputs = &outputs
	}
	payload, err := codec.MarshalStable(commit)
	if err != nil {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("encode Anthropic native commit: %w", err)
	}
	if len(payload) > provider.MaxNativeCommitBytes {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("anthropic native commit exceeds size limit")
	}
	return provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, nativeCommitPayloadRevision, payload)
}

func decodeNativeCommit(envelope provider.NativeCommitEnvelope) (nativeHistoryEntry, error) {
	if envelope.Family() != domain.ProviderAnthropic || envelope.Wire() != messagesWire ||
		envelope.PayloadVersion() != nativeCommitPayloadRevision {
		return nativeHistoryEntry{}, fmt.Errorf("anthropic native commit boundary is incompatible")
	}
	payload := envelope.Payload()
	if len(payload) == 0 || len(payload) > provider.MaxNativeCommitBytes {
		return nativeHistoryEntry{}, fmt.Errorf("anthropic native commit size is invalid")
	}
	var commit nativeHistoryCommit
	if err := codec.UnmarshalRecord(payload, &commit); err != nil {
		return nativeHistoryEntry{}, fmt.Errorf("decode native commit: %w", err)
	}
	entry := nativeHistoryEntry{Kind: commit.Kind}
	entry.UserInputs = commit.UserInputs
	entry.InputMetadata = commit.InputMetadata
	if commit.InputMetadata != nil {
		entry.TurnID = commit.InputMetadata.TurnID
	}

	switch commit.Kind {
	case nativeHistoryUserInputs:
		if commit.Assistant != nil || commit.Metadata != nil || commit.ToolOutputs != nil {
			return nativeHistoryEntry{}, fmt.Errorf("user inputs contain unrelated fields")
		}
	case nativeHistorySample:
		if commit.Assistant == nil || commit.Metadata == nil || commit.ToolOutputs != nil {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic sample commit fields are invalid")
		}
		if err := validateRawUsageCommit(commit.Metadata.Usage); err != nil {
			return nativeHistoryEntry{}, err
		}
		entry.Assistant = commit.Assistant.clone()
		entry.Metadata = decodeMetadata(*commit.Metadata)
	case nativeHistoryToolOutputs:
		if len(commit.UserInputs) != 0 || commit.InputMetadata != nil || commit.Assistant != nil || commit.Metadata != nil || commit.ToolOutputs == nil {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic tool outputs commit fields are invalid")
		}
		entry.ToolOutputs = commit.ToolOutputs.clone()
	default:
		return nativeHistoryEntry{}, fmt.Errorf("anthropic native commit kind is unsupported")
	}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry.clone(), nil
}

func validateRawUsageCommit(usage rawUsageCommit) error {
	fields := [...]optionalUintCommit{
		usage.InputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.OutputTokens,
	}
	for _, field := range fields {
		if field.Known != (field.Value != nil) {
			return fmt.Errorf("anthropic native commit usage state is invalid")
		}
	}
	return nil
}

func validateNativeHistoryEntry(entry nativeHistoryEntry) error {
	switch entry.Kind {
	case nativeHistoryUserInputs:
		if entry.InputMetadata == nil || len(entry.UserInputs) == 0 || entry.Assistant.Role != "" || entry.ToolOutputs.Role != "" || entry.Metadata.ID != "" {
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
			if err := validateUserTextMessage(input); err != nil {
				return err
			}
		}

	case nativeHistorySample:
		if len(entry.UserInputs) != 0 || entry.InputMetadata != nil {
			return fmt.Errorf("sample contains user input fields")
		}

		if entry.Assistant.Role != roleAssistant || len(entry.Assistant.Content) == 0 ||
			entry.ToolOutputs.Role != "" || len(entry.ToolOutputs.Content) != 0 {
			return fmt.Errorf("anthropic native commit assistant message is invalid")
		}
		seenToolUseIDs := make(map[string]struct{})
		for _, block := range entry.Assistant.Content {
			if err := validateAssistantBlock(block); err != nil {
				return err
			}
			if block.Type == blockTypeToolUse {
				if _, exists := seenToolUseIDs[block.ID]; exists {
					return fmt.Errorf("anthropic tool use ID is duplicated")
				}
				seenToolUseIDs[block.ID] = struct{}{}
			}
		}
		if entry.Metadata.ID == "" || entry.Metadata.Model == "" {
			return fmt.Errorf("anthropic native commit message metadata is invalid")
		}
		if _, err := entry.Metadata.Usage.normalized(); err != nil {
			return fmt.Errorf("anthropic native commit usage is invalid: %w", err)
		}
	case nativeHistoryToolOutputs:
		if len(entry.UserInputs) != 0 || entry.InputMetadata != nil || entry.Assistant.Role != "" || len(entry.Assistant.Content) != 0 ||
			entry.ToolOutputs.Role != roleUser || len(entry.ToolOutputs.Content) == 0 || entry.Metadata.ID != "" {
			return fmt.Errorf("anthropic tool outputs entry fields are invalid")
		}
		seen := make(map[string]struct{}, len(entry.ToolOutputs.Content))
		for _, block := range entry.ToolOutputs.Content {
			if block.Type != blockTypeToolResult || block.ToolUseID == "" || !utf8.ValidString(block.Content) {
				return fmt.Errorf("anthropic tool result block is invalid")
			}
			if _, exists := seen[block.ToolUseID]; exists {
				return fmt.Errorf("anthropic tool result ID is duplicated")
			}
			seen[block.ToolUseID] = struct{}{}
		}
	default:
		return fmt.Errorf("anthropic native commit kind is unsupported")
	}
	return nil
}

func validateUserTextMessage(message nativeMessage) error {
	// 当前用户输入只有一个纯文本片段；原生 opaque 扩展仅允许模型输出持有。
	encoded, err := codec.MarshalStable(message)
	if err != nil {
		return fmt.Errorf("user input encoding is invalid")
	}
	var current struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := codec.UnmarshalRecord(encoded, &current); err != nil {
		return fmt.Errorf("user input shape is invalid")
	}
	if current.Role != "user" || len(current.Content) != 1 || current.Content[0].Type != "text" {
		return fmt.Errorf("user input content is invalid")
	}
	return domain.ValidateUserText(current.Content[0].Text)
}

func validateAssistantBlock(block NativeItem) error {
	if len(block.Raw) > 0 && !codec.Valid(block.Raw) {
		return fmt.Errorf("anthropic native commit content block raw JSON is invalid")
	}
	switch block.Type {
	case blockTypeText, blockTypeThinking, blockTypeRedactedThinking:
		return nil
	case blockTypeToolUse:
		if block.Name == "Write" {
			if _, err := tool.DecodeWriteInput(block.Input); err != nil {
				return fmt.Errorf("invalid native Write input")
			}
		}
		if block.ID == "" || block.Name == "" || len(block.Input) == 0 || !codec.Valid(block.Input) {
			return fmt.Errorf("anthropic tool use block is invalid")
		}
		var object map[string]json.RawMessage
		if err := codec.Unmarshal(block.Input, &object); err != nil || object == nil {
			return fmt.Errorf("anthropic tool use input must be an object")
		}
		return nil
	default:
		if len(block.Raw) > 0 {
			return nil
		}
		return fmt.Errorf("anthropic native commit content block is unsupported")
	}
}

func encodeMetadata(metadata messageMetadata) messageMetadataCommit {
	return messageMetadataCommit{
		ID: metadata.ID, Model: metadata.Model,
		StopReason: optionalStringCommit{Known: metadata.StopReason.Known, Value: metadata.StopReason.Value},
		Usage: rawUsageCommit{
			InputTokens:              encodeOptionalInt(metadata.Usage.InputTokens),
			CacheCreationInputTokens: encodeOptionalInt(metadata.Usage.CacheCreationInputTokens),
			CacheReadInputTokens:     encodeOptionalInt(metadata.Usage.CacheReadInputTokens),
			OutputTokens:             encodeOptionalInt(metadata.Usage.OutputTokens),
		},
	}
}

func decodeMetadata(metadata messageMetadataCommit) messageMetadata {
	return messageMetadata{
		ID: metadata.ID, Model: metadata.Model,
		StopReason: optionalString{Known: metadata.StopReason.Known, Value: metadata.StopReason.Value},
		Usage: rawUsage{
			InputTokens:              decodeOptionalInt(metadata.Usage.InputTokens),
			CacheCreationInputTokens: decodeOptionalInt(metadata.Usage.CacheCreationInputTokens),
			CacheReadInputTokens:     decodeOptionalInt(metadata.Usage.CacheReadInputTokens),
			OutputTokens:             decodeOptionalInt(metadata.Usage.OutputTokens),
		},
	}
}

func encodeOptionalInt(value optionalUint) optionalUintCommit {
	encoded := optionalUintCommit{Known: value.Known}
	if value.Known {
		copy := value.Value
		encoded.Value = &copy
	}
	return encoded
}

func decodeOptionalInt(value optionalUintCommit) optionalUint {
	if !value.Known || value.Value == nil {
		return optionalUint{Known: value.Known}
	}
	return optionalUint{Known: true, Value: *value.Value}
}
