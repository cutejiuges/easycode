// Package telemetry 提供结构化日志、脱敏和未来 OpenTelemetry 接入边界。
package telemetry

import (
	"io"
	"log/slog"
)

// TODO(P8): telemetry logger 仅为发布诊断与 OpenTelemetry 接入阶段保留，当前不得接入请求正文或 Session；由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除。

// NewLogger 创建 JSON slog logger，调用方必须传入经过审查的字段。
func NewLogger(output io.Writer, level slog.Leveler) *slog.Logger {
	options := &slog.HandlerOptions{Level: level}
	return slog.New(slog.NewJSONHandler(output, options))
}
