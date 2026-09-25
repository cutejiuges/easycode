package openai

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// ContentPart 保存 OpenAI message item 中的文本内容。
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ReasoningSummaryPart 保存 OpenAI reasoning item 中的 summary 内容。
type ReasoningSummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// NativeItem 保存 OpenAI Responses 的原生 item。
// Raw 只用于无损回放服务端返回的完整 item，不向共享层传播。
type NativeItem struct {
	Type             string                 `json:"type"`
	ID               string                 `json:"id,omitempty"`
	Role             string                 `json:"role,omitempty"`
	Phase            string                 `json:"phase,omitempty"`
	Content          []ContentPart          `json:"content,omitempty"`
	ReasoningSummary []ReasoningSummaryPart `json:"summary,omitempty"`
	EncryptedContent string                 `json:"encrypted_content,omitempty"`
	Raw              json.RawMessage        `json:"-"`
}

type nativeItemWire struct {
	Type             string                 `json:"type"`
	ID               string                 `json:"id,omitempty"`
	Role             string                 `json:"role,omitempty"`
	Phase            string                 `json:"phase,omitempty"`
	Content          []ContentPart          `json:"content,omitempty"`
	ReasoningSummary []ReasoningSummaryPart `json:"summary,omitempty"`
	EncryptedContent string                 `json:"encrypted_content,omitempty"`
}

// NewUserItem 创建 Responses input 使用的用户文本 item。
func NewUserItem(text string) NativeItem {
	return NativeItem{
		Type: "message",
		Role: "user",
		Content: []ContentPart{{
			Type: "input_text",
			Text: text,
		}},
	}
}

// ProviderFamily 返回原生项所属协议家族。
func (NativeItem) ProviderFamily() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// ItemKind 返回原生 response item 类型。
func (item NativeItem) ItemKind() string {
	return item.Type
}

// MarshalJSON 优先回放服务端原始 item，避免未知扩展丢失。
func (item NativeItem) MarshalJSON() ([]byte, error) {
	if len(item.Raw) > 0 {
		return append([]byte(nil), item.Raw...), nil
	}
	return codec.MarshalStable(nativeItemWire{
		Type:             item.Type,
		ID:               item.ID,
		Role:             item.Role,
		Phase:            item.Phase,
		Content:          item.Content,
		ReasoningSummary: item.ReasoningSummary,
		EncryptedContent: item.EncryptedContent,
	})
}

// UnmarshalJSON 解码已知字段并保留完整原始 item。
func (item *NativeItem) UnmarshalJSON(data []byte) error {
	var wire nativeItemWire
	if err := codec.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("decode OpenAI response item: %w", err)
	}
	if wire.Type == "" {
		return fmt.Errorf("OpenAI response item type is required")
	}
	*item = NativeItem{
		Type:             wire.Type,
		ID:               wire.ID,
		Role:             wire.Role,
		Phase:            wire.Phase,
		Content:          append([]ContentPart(nil), wire.Content...),
		ReasoningSummary: append([]ReasoningSummaryPart(nil), wire.ReasoningSummary...),
		EncryptedContent: wire.EncryptedContent,
		Raw:              append(json.RawMessage(nil), data...),
	}
	return nil
}

func (item NativeItem) clone() NativeItem {
	item.Content = append([]ContentPart(nil), item.Content...)
	item.ReasoningSummary = append([]ReasoningSummaryPart(nil), item.ReasoningSummary...)
	item.Raw = append(json.RawMessage(nil), item.Raw...)
	return item
}
