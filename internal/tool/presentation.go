package tool

import (
	"easycode/internal/protocol"
	"strings"
	"unicode/utf8"
)

// ActivityText 为瞬态显示副本保留完整 UTF-8 前缀。
func ActivityText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// ProjectActivity 将完整调用映射到 Provider 无关的展示值，不读取外部资源。
func ProjectActivity(invocation Invocation) protocol.ToolActivityPayload {
	p := protocol.ToolActivityPayload{InvocationID: string(invocation.InvocationID()), Status: "waiting"}
	switch invocation.Capability() {
	case CapabilityRead:
		v, _ := invocation.Read()
		p.Tool = protocol.ToolRead
		p.Target = v.input.filePath
		p.Summary = p.Target
	case CapabilityGlob:
		v, _ := invocation.Glob()
		p.Tool = protocol.ToolGlob
		p.Target = v.Input().Path()
		p.Summary = v.Input().Pattern()
	case CapabilityGrep:
		v, _ := invocation.Grep()
		p.Tool = protocol.ToolGrep
		p.Target = v.Input().Path()
		p.Summary = v.Input().Pattern()
	case CapabilityPatch:
		v, _ := invocation.Patch()
		p.Tool = protocol.ToolPatch
		p.Target = v.input.filePath
		p.Summary = p.Target
	case CapabilityWrite:
		v, _ := invocation.Write()
		p.Tool = protocol.ToolWrite
		p.Target = v.input.filePath
		p.Summary = p.Target
	case CapabilityExec:
		v, _ := invocation.Exec()
		p.Tool = protocol.ToolShell
		p.Workdir = v.Input().Workdir()
		p.Target = p.Workdir
		p.Summary = v.Input().Command()
	case CapabilityReadArtifact:
		v, _ := invocation.ReadArtifact()
		p.Tool = protocol.ToolReadArtifact
		p.Summary = "Saved output"
		p.Target = string(v.Input().ID())
	}
	p.Summary = strings.Join(strings.Fields(p.Summary), " ")
	runes := []rune(p.Summary)
	if len(runes) > 160 {
		p.Summary = string(runes[:159]) + "…"
	}
	if p.Summary == "" {
		p.Summary = string(p.Tool)
	}
	p.Target = ActivityText(p.Target, 4096)
	return p
}

// ProjectOutputSaving 复制已冻结的保存事实，供宿主和实时终态共用。
func ProjectOutputSaving(saving ArtifactSaving, observed int64) protocol.ToolOutputInfo {
	info := protocol.ToolOutputInfo{Summary: protocol.ToolOutputSummary{Status: string(saving.Status()), Reason: saving.Reason(), ObservedBytes: observed}}
	if d, ok := saving.Descriptor(); ok {
		info.Summary.Readable = true
		info.Summary.SavedBytes = d.SavedBytes()
		info.Summary.ObservedBytes = d.ObservedBytes()
		info.Summary.Complete = d.Complete()
		info.Reference = &protocol.ToolOutputReference{ID: string(d.ID()), ThreadID: d.ThreadID(), InvocationID: string(d.InvocationID()), Source: protocol.ToolOutputSource(d.Stream()), SavedBytes: d.SavedBytes(), ObservedBytes: d.ObservedBytes(), SHA256: d.Digest(), Complete: d.Complete()}
	}
	return info
}
