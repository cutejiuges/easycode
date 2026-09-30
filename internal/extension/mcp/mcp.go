// Package mcp 定义 MCP server 和动态工具发现边界。
package mcp

import (
	"context"
	"encoding/json"
)

// TODO(P6): MCP 客户端边界仅为扩展系统阶段保留，当前不得启动进程或连接远端；由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除。

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
