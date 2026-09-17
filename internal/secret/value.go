// Package secret 提供默认不可打印的敏感值对象。
package secret

import (
	"log/slog"
)

const redacted = "[REDACTED]"

// Value 保存敏感字符串，并阻止常见日志和序列化路径泄漏原文。
type Value struct {
	raw string
}

// New 创建敏感值对象。
func New(raw string) Value {
	return Value{raw: raw}
}

// Reveal 仅供实际鉴权边界读取原始值。
func (value Value) Reveal() string {
	return value.raw
}

// Empty 判断敏感值是否为空。
func (value Value) Empty() bool {
	return value.raw == ""
}

// String 始终返回脱敏文本。
func (Value) String() string {
	return redacted
}

// GoString 阻止 %#v 等调试格式泄漏原始值。
func (Value) GoString() string {
	return redacted
}

// MarshalJSON 阻止配置诊断或 Session 序列化泄漏原始值。
func (Value) MarshalJSON() ([]byte, error) {
	return []byte(`"[REDACTED]"`), nil
}

// LogValue 为 slog 提供脱敏值。
func (Value) LogValue() slog.Value {
	return slog.StringValue(redacted)
}
