// Package telemetry 提供结构化日志、脱敏和未来 OpenTelemetry 接入边界。
package telemetry

import (
	"io"
	"log/slog"
)

// NewLogger 创建 JSON slog logger，调用方必须传入经过审查的字段。
func NewLogger(output io.Writer, level slog.Leveler) *slog.Logger {
	options := &slog.HandlerOptions{Level: level}
	return slog.New(slog.NewJSONHandler(output, options))
}
