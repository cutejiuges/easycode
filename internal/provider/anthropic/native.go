package anthropic

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const (
	blockTypeText             = "text"
	blockTypeThinking         = "thinking"
	blockTypeRedactedThinking = "redacted_thinking"
	blockTypeToolUse          = "tool_use"
	blockTypeToolResult       = "tool_result"

	roleUser      = "user"
	roleAssistant = "assistant"
)

// NativeItem 保存 Anthropic Messages 的完成 content block。
// Raw 仅用于在 Anthropic 包内无损回放服务端返回的 opaque block。
type NativeItem struct {
	Type         string          `json:"-"`
	Text         string          `json:"-"`
	Thinking     string          `json:"-"`
	Signature    string          `json:"-"`
	RedactedData string          `json:"-"`
	ID           string          `json:"-"`
	Name         string          `json:"-"`
	Input        json.RawMessage `json:"-"`
	ToolUseID    string          `json:"-"`
	Content      string          `json:"-"`
	IsError      bool            `json:"-"`
	Raw          json.RawMessage `json:"-"`
}

type nativeItemWire struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ProviderFamily 返回原生项所属协议家族。
func (NativeItem) ProviderFamily() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// ItemKind 返回原生 content block 类型。
func (item NativeItem) ItemKind() string {
	return item.Type
}

// MarshalJSON 生成 Messages request 可直接回放的 content block。
func (item NativeItem) MarshalJSON() ([]byte, error) {
	if len(item.Raw) > 0 {
		return append([]byte(nil), item.Raw...), nil
	}
	switch item.Type {
	case blockTypeText:
		return codec.MarshalStable(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: item.Type, Text: item.Text})
	case blockTypeThinking:
		return codec.MarshalStable(struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
		}{Type: item.Type, Thinking: item.Thinking, Signature: item.Signature})
	case blockTypeRedactedThinking:
		return codec.MarshalStable(struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}{Type: item.Type, Data: item.RedactedData})
	case blockTypeToolUse:
		return codec.MarshalStable(struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}{Type: item.Type, ID: item.ID, Name: item.Name, Input: append(json.RawMessage(nil), item.Input...)})
	case blockTypeToolResult:
		return codec.MarshalStable(struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   string `json:"content"`
			IsError   bool   `json:"is_error,omitempty"`
		}{Type: item.Type, ToolUseID: item.ToolUseID, Content: item.Content, IsError: item.IsError})
	case "":
		return nil, fmt.Errorf("anthropic content block type is required")
	default:
		return nil, fmt.Errorf("anthropic content block type is unsupported")
	}
}

// UnmarshalJSON 解码已知字段并保留完整原始 block。
func (item *NativeItem) UnmarshalJSON(data []byte) error {
	var wire nativeItemWire
	if err := codec.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("decode Anthropic content block: %w", err)
	}
	if wire.Type == "" {
		return fmt.Errorf("anthropic content block type is required")
	}
	*item = NativeItem{
		Type:         wire.Type,
		Text:         wire.Text,
		Thinking:     wire.Thinking,
		Signature:    wire.Signature,
		RedactedData: wire.Data,
		ID:           wire.ID,
		Name:         wire.Name,
		Input:        append(json.RawMessage(nil), wire.Input...),
		ToolUseID:    wire.ToolUseID,
		Content:      wire.Content,
		IsError:      wire.IsError,
		Raw:          append(json.RawMessage(nil), data...),
	}
	return nil
}

func (item NativeItem) clone() NativeItem {
	item.Input = append(json.RawMessage(nil), item.Input...)
	item.Raw = append(json.RawMessage(nil), item.Raw...)
	return item
}

// nativeMessage 保存下一次 Messages request 使用的原生消息边界。
type nativeMessage struct {
	Role    string       `json:"role"`
	Content []NativeItem `json:"content"`
}

func newUserMessage(text string) nativeMessage {
	return nativeMessage{
		Role: roleUser,
		Content: []NativeItem{{
			Type: blockTypeText,
			Text: text,
		}},
	}
}

func (message nativeMessage) clone() nativeMessage {
	cloned := nativeMessage{Role: message.Role, Content: make([]NativeItem, 0, len(message.Content))}
	for _, item := range message.Content {
		cloned.Content = append(cloned.Content, item.clone())
	}
	return cloned
}

// optionalUint 区分服务端明确给出的零值与缺失字段。
type optionalUint struct {
	Value uint64
	Known bool
}

// optionalString 区分服务端给出的字符串与缺失字段。
type optionalString struct {
	Value string
	Known bool
}

// rawUsage 原样保存当前文本切片关心的 Anthropic usage 字段。
type rawUsage struct {
	InputTokens              optionalUint
	CacheCreationInputTokens optionalUint
	CacheReadInputTokens     optionalUint
	OutputTokens             optionalUint
}

func (usage rawUsage) clone() rawUsage {
	return usage
}

func (usage rawUsage) normalized() (domain.SampleUsage, error) {
	return domain.NewSampleUsage(
		anthropicUsageMetric(usage.InputTokens),
		anthropicUsageMetric(usage.CacheReadInputTokens),
		anthropicUsageMetric(usage.CacheCreationInputTokens),
		anthropicUsageMetric(usage.OutputTokens),
		domain.NotApplicableUsageMetric(),
	)
}

func anthropicUsageMetric(value optionalUint) domain.UsageMetric {
	if !value.Known {
		return domain.UnknownUsageMetric()
	}
	return domain.KnownUsageMetric(value.Value)
}

// messageMetadata 保存 response message 自身的信息，不参与下一轮 content 编译。
type messageMetadata struct {
	ID         string
	Model      string
	StopReason optionalString
	Usage      rawUsage
}

func (metadata messageMetadata) clone() messageMetadata {
	metadata.Usage = metadata.Usage.clone()
	return metadata
}

type nativeHistoryEntryKind string

const (
	nativeHistorySample      nativeHistoryEntryKind = "sample"
	nativeHistoryToolOutputs nativeHistoryEntryKind = "tool_outputs"
)

// nativeHistoryEntry 是 Messages 原生历史唯一的当前内存模型。
type nativeHistoryEntry struct {
	Kind        nativeHistoryEntryKind
	Input       *nativeMessage
	Assistant   nativeMessage
	ToolOutputs nativeMessage
	Metadata    messageMetadata
}

func (entry nativeHistoryEntry) clone() nativeHistoryEntry {
	cloned := nativeHistoryEntry{
		Kind: entry.Kind, Assistant: entry.Assistant.clone(),
		ToolOutputs: entry.ToolOutputs.clone(), Metadata: entry.Metadata.clone(),
	}
	if entry.Input != nil {
		input := entry.Input.clone()
		cloned.Input = &input
	}
	return cloned
}
