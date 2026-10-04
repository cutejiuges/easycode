package openai

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
	responsesWire               = "responses"
	nativeCommitPayloadV1       = 1
	nativeCommitShapeTextSample = "text_sample"
)

type textSampleCommit struct {
	Shape       string          `json:"shape"`
	User        NativeItem      `json:"user"`
	OutputItems []NativeItem    `json:"output_items"`
	Usage       *rawUsageCommit `json:"usage"`
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

func encodeNativeCommit(turn nativeTurn) (provider.NativeCommitEnvelope, error) {
	cloned := turn.clone()
	if err := validateNativeTurn(cloned); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	payload, err := codec.MarshalStable(textSampleCommit{
		Shape: nativeCommitShapeTextSample, User: cloned.User, OutputItems: cloned.Outputs,
		Usage: encodeRawUsage(cloned.Usage),
	})
	if err != nil {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("encode OpenAI native commit: %w", err)
	}
	if len(payload) > provider.MaxNativeCommitBytes {
		return provider.NativeCommitEnvelope{}, fmt.Errorf("OpenAI native commit exceeds size limit")
	}
	return provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, responsesWire, nativeCommitPayloadV1, payload,
	)
}

func decodeNativeCommit(envelope provider.NativeCommitEnvelope) (nativeTurn, error) {
	if envelope.Family() != domain.ProviderOpenAI || envelope.Wire() != responsesWire ||
		envelope.PayloadVersion() != nativeCommitPayloadV1 {
		return nativeTurn{}, fmt.Errorf("OpenAI native commit boundary is incompatible")
	}
	payload := envelope.Payload()
	if len(payload) == 0 || len(payload) > provider.MaxNativeCommitBytes {
		return nativeTurn{}, fmt.Errorf("OpenAI native commit size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var commit textSampleCommit
	if err := decoder.Decode(&commit); err != nil {
		return nativeTurn{}, fmt.Errorf("decode OpenAI native commit: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nativeTurn{}, fmt.Errorf("OpenAI native commit contains trailing JSON")
	}
	if commit.Shape != nativeCommitShapeTextSample {
		return nativeTurn{}, fmt.Errorf("OpenAI native commit shape is unsupported")
	}
	usage, err := decodeRawUsage(commit.Usage)
	if err != nil {
		return nativeTurn{}, err
	}
	turn := nativeTurn{User: commit.User.clone(), Outputs: cloneNativeItems(commit.OutputItems), Usage: usage}
	if err := validateNativeTurn(turn); err != nil {
		return nativeTurn{}, err
	}
	return turn.clone(), nil
}

func validateNativeTurn(turn nativeTurn) error {
	if turn.User.Type != "message" || turn.User.Role != "user" || len(turn.User.Content) == 0 {
		return fmt.Errorf("OpenAI native commit user item is invalid")
	}
	for _, part := range turn.User.Content {
		if part.Type != "input_text" {
			return fmt.Errorf("OpenAI native commit user content is invalid")
		}
	}
	if len(turn.Outputs) == 0 {
		return fmt.Errorf("OpenAI native commit output items are required")
	}
	for _, item := range turn.Outputs {
		if item.Type == "" {
			return fmt.Errorf("OpenAI native commit output item type is required")
		}
		if len(item.Raw) > 0 && !json.Valid(item.Raw) {
			return fmt.Errorf("OpenAI native commit output item raw JSON is invalid")
		}
		if item.Type == "message" && item.Role != "assistant" {
			return fmt.Errorf("OpenAI native commit message role is invalid")
		}
	}
	if _, err := turn.Usage.normalized(); err != nil {
		return fmt.Errorf("OpenAI native commit usage is invalid: %w", err)
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
