package anthropic

import (
	"encoding/json"

	"easycode/internal/domain"
)

// NativeItem 保存 Anthropic Messages 的完成 content block。
type NativeItem struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// ProviderFamily 返回原生项所属协议家族。
func (NativeItem) ProviderFamily() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// ItemKind 返回原生 content block 类型。
func (item NativeItem) ItemKind() string {
	return item.Type
}
