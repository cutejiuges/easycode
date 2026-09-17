// Package mcp 定义 MCP server 和动态工具发现边界。
package mcp

import (
	"context"
	"encoding/json"
)

// ServerConfig 描述 MCP server 的本地启动或远程连接配置。
type ServerConfig struct {
	Name      string
	Transport string
	Command   []string
	URL       string
}

// Tool 描述 MCP 动态发现的工具。
type Tool struct {
	Server      string
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Client 提供 MCP 生命周期和工具发现能力。
type Client interface {
	Start(context.Context, ServerConfig) error
	Tools(context.Context, string) ([]Tool, error)
	Close(context.Context, string) error
}
