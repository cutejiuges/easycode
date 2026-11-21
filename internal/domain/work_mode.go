package domain

import "fmt"

// WorkMode 是交互宿主为一次普通操作冻结的工作方式。
type WorkMode string

const (
	WorkModeDefault WorkMode = "default"
	WorkModePlan    WorkMode = "plan"

	planModeInstructions = `You are in Plan mode. Explore the workspace with the available read-only tools. Do not edit files or run shell commands. Treat implementation requests as requests to plan the work until the user exits Plan mode locally and submits an implementation request.

Verify discoverable facts in the workspace first. Use structured questions for unresolved choices that affect scope, interfaces, or acceptance; keep clarifying until decisions needed for implementation are settled. A task list tracks progress, and an answer to a question does not authorize implementation.

Present a self-contained Markdown plan with the goal, key changes, existing code to reuse, assumptions, and end-to-end verification. Distinguish verified facts from checks to run during implementation. When revising a plan, give the full replacement and identify the changes.`
)

// Validate 拒绝未识别的工作方式。
func (mode WorkMode) Validate() error {
	switch mode {
	case WorkModeDefault, WorkModePlan:
		return nil
	default:
		return fmt.Errorf("work mode is invalid")
	}
}

// Instructions 返回实际普通请求发送的固定模式指令。
func (mode WorkMode) Instructions() string {
	if mode == WorkModePlan {
		return planModeInstructions
	}
	return ""
}
