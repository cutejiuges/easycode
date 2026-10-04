package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

const (
	messagesWire                = "messages"
	nativeCommitPayloadV1       = 1
	nativeCommitShapeTextSample = "text_sample"
)

type textSampleCommit struct {
	Shape     string                `json:"shape"`
	User      nativeMessage         `json:"user"`
	Assistant nativeMessage         `json:"assistant"`
	Metadata  messageMetadataCommit `json:"metadata"`
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

func encodeNativeCommit(turn nativeTurn) (provider.NativeCommitEnvelope, error) {
	cloned := turn.clone()
	if err := validateNativeTurn(cloned); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	payload, err := codec.MarshalStable(textSampleCommit{
		Shape: nativeCommitShapeTextSample, User: cloned.User, Assistant: cloned.Assistant,
		Metadata: encodeMetadata(cloned.Metadata),
	})
	if err != nil {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("encode Anthropic native commit: %w", err)
	}
	if len(payload) > provider.MaxNativeCommitBytes {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("anthropic native commit exceeds size limit")
	}
	return provider.NewNativeCommitEnvelope(
		domain.ProviderAnthropic, messagesWire, nativeCommitPayloadV1, payload,
	)
}

func decodeNativeCommit(envelope provider.NativeCommitEnvelope) (nativeTurn, error) {
	if envelope.Family() != domain.ProviderAnthropic || envelope.Wire() != messagesWire ||
		envelope.PayloadVersion() != nativeCommitPayloadV1 {
		return nativeTurn{}, fmt.Errorf("anthropic native commit boundary is incompatible")
	}
	payload := envelope.Payload()
	if len(payload) == 0 || len(payload) > provider.MaxNativeCommitBytes {
		return nativeTurn{}, fmt.Errorf("anthropic native commit size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var commit textSampleCommit
	if err := decoder.Decode(&commit); err != nil {
		return nativeTurn{}, fmt.Errorf("decode Anthropic native commit: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nativeTurn{}, fmt.Errorf("anthropic native commit contains trailing JSON")
	}
	if commit.Shape != nativeCommitShapeTextSample {
		return nativeTurn{}, fmt.Errorf("anthropic native commit shape is unsupported")
	}
	if err := validateRawUsageCommit(commit.Metadata.Usage); err != nil {
		return nativeTurn{}, err
	}
	turn := nativeTurn{
		User: commit.User.clone(), Assistant: commit.Assistant.clone(),
		Metadata: decodeMetadata(commit.Metadata),
	}
	if err := validateNativeTurn(turn); err != nil {
		return nativeTurn{}, err
	}
	return turn.clone(), nil
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

func validateNativeTurn(turn nativeTurn) error {
	if turn.User.Role != roleUser || len(turn.User.Content) == 0 {
		return fmt.Errorf("anthropic native commit user message is invalid")
	}
	for _, block := range turn.User.Content {
		if block.Type != blockTypeText {
			return fmt.Errorf("anthropic native commit user content is invalid")
		}
	}
	if turn.Assistant.Role != roleAssistant || len(turn.Assistant.Content) == 0 {
		return fmt.Errorf("anthropic native commit assistant message is invalid")
	}
	for _, block := range turn.Assistant.Content {
		if block.Type == "" {
			return fmt.Errorf("anthropic native commit content block type is required")
		}
		if len(block.Raw) > 0 && !json.Valid(block.Raw) {
			return fmt.Errorf("anthropic native commit content block raw JSON is invalid")
		}
		if block.Type != blockTypeText && block.Type != blockTypeThinking &&
			block.Type != blockTypeRedactedThinking && len(block.Raw) == 0 {
			return fmt.Errorf("anthropic native commit content block is unsupported")
		}
	}
	if turn.Metadata.ID == "" || turn.Metadata.Model == "" {
		return fmt.Errorf("anthropic native commit message metadata is invalid")
	}
	if _, err := turn.Metadata.Usage.normalized(); err != nil {
		return fmt.Errorf("anthropic native commit usage is invalid: %w", err)
	}
	return nil
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
