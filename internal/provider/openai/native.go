package openai

import (
	"encoding/json"

	"easycode/internal/domain"
)

// NativeItem 保存 OpenAI Responses 的完成 response item。
type NativeItem struct {
	Type             string          `json:"type"`
	Phase            string          `json:"phase,omitempty"`
	Text             string          `json:"text,omitempty"`
	ReasoningSummary []string        `json:"reasoning_summary,omitempty"`
	EncryptedContent string          `json:"encrypted_content,omitempty"`
	Raw              json.RawMessage `json:"raw,omitempty"`
}

// ProviderFamily 返回原生项所属协议家族。
func (NativeItem) ProviderFamily() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// ItemKind 返回原生 response item 类型。
func (item NativeItem) ItemKind() string {
	return item.Type
}
