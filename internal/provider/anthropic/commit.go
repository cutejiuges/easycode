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
	messagesWire = domain.AnthropicMessagesWire
)

type nativeHistoryCommit struct {
	SessionStartInput    *nativeMessage                 `json:"session_start_input,omitempty"`
	SessionStartMetadata *provider.SessionStartMetadata `json:"session_start_metadata,omitempty"`
	FeedbackInput        *nativeMessage                 `json:"feedback_input,omitempty"`
	FeedbackMetadata     *provider.HookFeedbackMetadata `json:"feedback_metadata,omitempty"`
	Kind                 nativeHistoryEntryKind         `json:"kind"`
	UserInputs           []nativeMessage                `json:"user_inputs,omitempty"`
	InputMetadata        *provider.InputMetadata        `json:"input_metadata,omitempty"`
	Assistant            *nativeMessage                 `json:"assistant,omitempty"`
	ToolOutputs          *nativeMessage                 `json:"tool_outputs,omitempty"`
	Metadata             *messageMetadataCommit         `json:"metadata,omitempty"`
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

type optionalUintCommit = provider.OptionalUintCommit

type rawUsageCommit struct {
	InputTokens              optionalUintCommit `json:"input_tokens"`
	CacheCreationInputTokens optionalUintCommit `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     optionalUintCommit `json:"cache_read_input_tokens"`
	OutputTokens             optionalUintCommit `json:"output_tokens"`
	ThinkingTokens           optionalUintCommit `json:"thinking_tokens"`
}

func encodeNativeCommit(entry nativeHistoryEntry) (provider.NativeCommitEnvelope, error) {
	cloned := entry.clone()
	if err := validateNativeHistoryEntry(cloned); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	commit := nativeHistoryCommit{Kind: cloned.Kind, UserInputs: cloned.UserInputs, InputMetadata: cloned.InputMetadata, FeedbackInput: cloned.FeedbackInput, FeedbackMetadata: cloned.FeedbackMetadata, SessionStartInput: cloned.SessionStartInput, SessionStartMetadata: cloned.SessionStartMetadata}
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
	return provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, payload)
}

func decodeNativeCommit(envelope provider.NativeCommitEnvelope) (nativeHistoryEntry, error) {
	if envelope.Family() != domain.ProviderAnthropic || envelope.Wire() != messagesWire {
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
	entry.FeedbackInput = commit.FeedbackInput
	entry.SessionStartInput = commit.SessionStartInput
	entry.SessionStartMetadata = commit.SessionStartMetadata
	entry.TurnID = entry.NativeSessionStartTarget().TurnID
	entry.FeedbackMetadata = commit.FeedbackMetadata
	entry.UserInputs = commit.UserInputs
	entry.InputMetadata = commit.InputMetadata
	if commit.InputMetadata != nil {
		entry.TurnID = commit.InputMetadata.TurnID
	}

	switch commit.Kind {
	case nativeHistorySessionStart, nativeHistoryHookFeedback:
		if len(commit.UserInputs) != 0 || commit.InputMetadata != nil || commit.Assistant != nil || commit.Metadata != nil || commit.ToolOutputs != nil {
			return nativeHistoryEntry{}, fmt.Errorf("hook feedback contains unrelated fields")
		}
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
		entry.Assistant = *commit.Assistant
		entry.Metadata = decodeMetadata(*commit.Metadata)
	case nativeHistoryToolOutputs:
		if len(commit.UserInputs) != 0 || commit.InputMetadata != nil || commit.Assistant != nil || commit.Metadata != nil || commit.ToolOutputs == nil {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic tool outputs commit fields are invalid")
		}
		entry.ToolOutputs = *commit.ToolOutputs
	default:
		return nativeHistoryEntry{}, fmt.Errorf("anthropic native commit kind is unsupported")
	}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func validateRawUsageCommit(usage rawUsageCommit) error {
	fields := [...]optionalUintCommit{
		usage.InputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.OutputTokens, usage.ThinkingTokens,
	}
	for _, field := range fields {
		if field.Known != (field.Value != nil) {
			return fmt.Errorf("anthropic native commit usage state is invalid")
		}
	}
	return nil
}

func validateNativeHistoryEntry(entry nativeHistoryEntry) error {
	if entry.Kind != nativeHistorySessionStart && (entry.SessionStartInput != nil || entry.SessionStartMetadata != nil) {
		return fmt.Errorf("unrelated session start context fields")
	}
	if entry.Kind != nativeHistoryHookFeedback && (entry.FeedbackInput != nil || entry.FeedbackMetadata != nil) {
		return fmt.Errorf("unrelated hook feedback fields")
	}
	switch entry.Kind {
	case nativeHistorySessionStart:
		if len(entry.UserInputs) != 0 || entry.InputMetadata != nil || entry.Assistant.Role != "" || len(entry.Assistant.Content) != 0 || entry.ToolOutputs.Role != "" || len(entry.ToolOutputs.Content) != 0 || entry.Metadata != (messageMetadata{}) {
			return fmt.Errorf("session start context contains unrelated fields")
		}
		if _, err := entrySessionStartContext(entry); err != nil {
			return err
		}
	case nativeHistoryHookFeedback:
		if len(entry.UserInputs) != 0 || entry.InputMetadata != nil || entry.Assistant.Role != "" || len(entry.Assistant.Content) != 0 || entry.ToolOutputs.Role != "" || len(entry.ToolOutputs.Content) != 0 || entry.Metadata != (messageMetadata{}) {
			return fmt.Errorf("hook feedback contains unrelated fields")
		}
		if _, err := entryStopFeedback(entry); err != nil {
			return err
		}
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
		for i, input := range entry.UserInputs {
			if len(input.Content) == 0 {
				return fmt.Errorf("user input content is required")
			}
			expected, err := entry.InputMetadata.TextBlocks(entry.InputMetadata.InputIDs[i], input.Content[0].Text)
			if err != nil {
				return err
			}
			if len(input.Content) != len(expected) {
				return fmt.Errorf("input attachment count mismatch")
			}
			for index, text := range expected {
				if input.Content[index].Text != text {
					return fmt.Errorf("input attachment content mismatch")
				}
			}
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
	// 原文、可选 Skill 和文件都是有序文本；opaque 扩展仅允许模型输出持有。
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
	if current.Role != "user" || len(current.Content) < 1 || len(current.Content) > 2+domain.MaxFileReferences {
		return fmt.Errorf("user input content is invalid")
	}
	for _, part := range current.Content {
		if part.Type != "text" {
			return fmt.Errorf("user input block type is invalid")
		}
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
		if block.Name == "AskUserQuestion" {
			if _, err := tool.DecodeQuestionInput(block.Input); err != nil {
				return fmt.Errorf("invalid native question input")
			}
		}
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
			InputTokens:              metadata.Usage.InputTokens.Commit(),
			CacheCreationInputTokens: metadata.Usage.CacheCreationInputTokens.Commit(),
			CacheReadInputTokens:     metadata.Usage.CacheReadInputTokens.Commit(),
			OutputTokens:             metadata.Usage.OutputTokens.Commit(),
			ThinkingTokens:           metadata.Usage.ThinkingTokens.Commit(),
		},
	}
}

func decodeMetadata(metadata messageMetadataCommit) messageMetadata {
	return messageMetadata{
		ID: metadata.ID, Model: metadata.Model,
		StopReason: optionalString{Known: metadata.StopReason.Known, Value: metadata.StopReason.Value},
		Usage: rawUsage{
			InputTokens:              metadata.Usage.InputTokens.OptionalUint(),
			CacheCreationInputTokens: metadata.Usage.CacheCreationInputTokens.OptionalUint(),
			CacheReadInputTokens:     metadata.Usage.CacheReadInputTokens.OptionalUint(),
			OutputTokens:             metadata.Usage.OutputTokens.OptionalUint(),
			ThinkingTokens:           metadata.Usage.ThinkingTokens.OptionalUint(),
		},
	}
}
