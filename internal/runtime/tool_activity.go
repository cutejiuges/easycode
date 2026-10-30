package runtime

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

func activityForInvocation(invocation tool.Invocation) protocol.ToolActivityPayload {
	return tool.ProjectActivity(invocation)
}

func (runtime *Runtime) emitToolStarted(turn domain.TurnID, invocation tool.Invocation) {
	if runtime.activityEmitter == nil {
		return
	}
	p := activityForInvocation(invocation)
	p.Status = "running"
	if event, err := protocol.NewToolStarted(p); err == nil {
		runtime.activityEmitter(runtime.decorate(event, turn))
	}
}

// notifyToolResult 只在 result Sync 成功后发布；进度永远不能替代此终态。
func (runtime *Runtime) notifyToolResult(turn domain.TurnID, result tool.InvocationResult) {
	if runtime.toolHistory != nil {
		runtime.toolHistory.ToolResult(result)
	}
	if runtime.activityEmitter == nil {
		return
	}
	invocation, ok := runtime.activityInvocations[result.InvocationID()]
	if !ok {
		return
	}
	p := activityForInvocation(invocation)
	p.Status, p.Code = string(result.Status()), result.Code()
	if result.Capability() == tool.CapabilityRead {
		p.Target = result.ReadMetadata().RelativePath()
	}
	if result.Capability() == tool.CapabilityExec {
		m := result.ExecMetadata()
		p.ElapsedMillis = m.DurationMillis()
		p.StdoutBytes = m.Stdout().ObservedBytes()
		p.StderrBytes = m.Stderr().ObservedBytes()
		p.ExitCode, p.ExitKnown = m.ExitCode()
		p.Truncated = m.OutputTruncated()
	} else {
		p.Truncated = result.Budget().Reason() == tool.BudgetShortened
	}
	preview := tool.ActivityText(result.Preview().Text(), 4096)
	terminal := protocol.ToolTerminalPayload{ToolActivityPayload: p, ResultPreview: preview}
	switch result.Capability() {
	case tool.CapabilityExec:
		m := result.ExecMetadata()
		terminal.Stdout = tool.ProjectOutputSaving(m.StdoutSaving(), m.Stdout().ObservedBytes())
		terminal.Stderr = tool.ProjectOutputSaving(m.StderrSaving(), m.Stderr().ObservedBytes())
	case tool.CapabilityGlob, tool.CapabilityGrep:
		terminal.Search = tool.ProjectOutputSaving(result.SearchSaving(), 0)
	}
	if event, err := protocol.NewToolTerminal(terminal); err == nil {
		runtime.activityEmitter(runtime.decorate(event, turn))
	}
}
