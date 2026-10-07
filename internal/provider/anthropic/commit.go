package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

const (
	messagesWire                = "messages"
	nativeCommitPayloadRevision = 1
)

type nativeHistoryCommit struct {
	Kind        nativeHistoryEntryKind `json:"kind"`
	Input       *nativeMessage         `json:"input,omitempty"`
	Assistant   *nativeMessage         `json:"assistant,omitempty"`
	ToolOutputs *nativeMessage         `json:"tool_outputs,omitempty"`
	Metadata    *messageMetadataCommit `json:"metadata,omitempty"`
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
	commit := nativeHistoryCommit{Kind: cloned.Kind, Input: cloned.Input}
	if cloned.Kind == nativeHistorySample {
		assistant := cloned.Assistant.clone()
		metadata := encodeMetadata(cloned.Metadata)
		commit.Assistant = &assistant
		commit.Metadata = &metadata
	} else {
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
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var commit nativeHistoryCommit
	if err := decoder.Decode(&commit); err != nil {
		return nativeHistoryEntry{}, fmt.Errorf("decode Anthropic native commit: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nativeHistoryEntry{}, fmt.Errorf("anthropic native commit contains trailing JSON")
	}
	entry := nativeHistoryEntry{Kind: commit.Kind}
	if commit.Input != nil {
		input := commit.Input.clone()
		entry.Input = &input
	}
	switch commit.Kind {
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
		if commit.Input != nil || commit.Assistant != nil || commit.Metadata != nil || commit.ToolOutputs == nil {
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
	case nativeHistorySample:
		if entry.Input != nil {
			if err := validateUserTextMessage(*entry.Input); err != nil {
				return err
			}
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
		if entry.Input != nil || entry.Assistant.Role != "" || len(entry.Assistant.Content) != 0 ||
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
	if message.Role != roleUser || len(message.Content) == 0 {
		return fmt.Errorf("anthropic native commit user message is invalid")
	}
	for _, block := range message.Content {
		if block.Type != blockTypeText {
			return fmt.Errorf("anthropic native commit user content is invalid")
		}
	}
	return nil
}

func validateAssistantBlock(block NativeItem) error {
	if len(block.Raw) > 0 && !json.Valid(block.Raw) {
		return fmt.Errorf("anthropic native commit content block raw JSON is invalid")
	}
	switch block.Type {
	case blockTypeText, blockTypeThinking, blockTypeRedactedThinking:
		return nil
	case blockTypeToolUse:
		if block.ID == "" || block.Name == "" || len(block.Input) == 0 || !json.Valid(block.Input) {
			return fmt.Errorf("anthropic tool use block is invalid")
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(block.Input, &object); err != nil || object == nil {
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
