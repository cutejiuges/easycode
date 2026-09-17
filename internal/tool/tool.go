// Package tool 定义工具能力、执行、权限和调度边界。
package tool

import (
	"context"
	"encoding/json"

	"easycode/internal/domain"
)

// Capability 是与 provider tool 名称解耦的共享能力标识。
type Capability string

const (
	CapabilityReadFile  Capability = "fs.read"
	CapabilityWriteFile Capability = "fs.write"
	CapabilityPatchFile Capability = "fs.patch"
	CapabilitySearch    Capability = "fs.search"
	CapabilityExec      Capability = "shell.exec"
)

// Spec 描述模型可见的工具 facade。
type Spec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Capability  Capability      `json:"capability"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Invocation 是进入执行层的完整且已校验工具调用。
type Invocation struct {
	CallID     domain.CallID
	Capability Capability
	Input      json.RawMessage
}

// Result 是与 provider 编码和 TUI 展示解耦的工具结果。
type Result struct {
	Output   json.RawMessage
	Artifact string
	Metadata json.RawMessage
}

// Executor 执行一个具体工具能力，不感知 TUI 和 provider wire。
type Executor interface {
	Execute(context.Context, Invocation) (Result, error)
}

// Decision 表示权限策略结果。
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionAsk   Decision = "ask"
	DecisionDeny  Decision = "deny"
)

// Policy 在工具产生副作用前完成权限和 sandbox 决策。
type Policy interface {
	Decide(context.Context, Invocation) (Decision, error)
}
