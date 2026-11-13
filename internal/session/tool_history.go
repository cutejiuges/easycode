package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// ToolHistoryEntry 保存已验证工具事实的纯内存投影，不保存文件句柄或实时进度。
type ToolHistoryEntry struct {
	NativeSequence uint64
	TurnID         domain.TurnID
	SampleIndex    uint32
	CallIndex      uint32
	Sequence       uint64
	Invocation     tool.Invocation
	Result         tool.InvocationResult
	HasResult      bool
	Hooks          []HookHistoryEntry
	PostHooks      *ReplayedPostHooks
	DetailSequence uint64
}

func projectToolHistory(records []Record) ([]ToolHistoryEntry, error) {
	entries := make([]ToolHistoryEntry, 0)
	indices := make(map[tool.InvocationID]int)
	var nativeSequence uint64
	plans := make(map[string]domain.HookPlan)
	for _, record := range records {
		switch record.EventKind {
		case EventProviderNativeCommit:
			nativeSequence = record.Sequence
		case EventToolCallReady:
			p, err := DecodeToolCallReadyPayload(record)
			if err != nil {
				return nil, err
			}
			inv, err := p.Domain()
			if err != nil {
				return nil, err
			}
			if _, exists := indices[inv.InvocationID()]; exists || nativeSequence == 0 {
				return nil, replayCorrupt("invalid tool ready projection")
			}
			indices[inv.InvocationID()] = len(entries)
			entries = append(entries, ToolHistoryEntry{NativeSequence: nativeSequence, TurnID: record.TurnID, SampleIndex: p.SampleIndex, CallIndex: p.CallIndex, Sequence: record.Sequence, Invocation: inv})
		case EventToolHookGatePlanned:
			p, err := DecodeToolHookGatePlanned(record)
			if err != nil {
				return nil, err
			}
			plans[string(p.Event)+":"+p.InvocationID+":"+p.Digest] = p
		case EventToolPostHookPlanned:
			p, err := DecodeToolPostHookPlanned(record)
			if err != nil {
				return nil, err
			}
			index, ok := indices[tool.InvocationID(p.InvocationID)]
			if !ok {
				return nil, replayCorrupt("post hook has no ready projection")
			}
			entries[index].PostHooks = &ReplayedPostHooks{Plan: p, PlanSequence: record.Sequence}
			entries[index].DetailSequence = record.Sequence
			for _, branch := range p.Branches {
				plan, err := domain.NewHookPlan(p.InvocationID, p.Capability, branch.Hooks)
				if err != nil {
					return nil, err
				}
				plans[string(branch.Event)+":"+p.InvocationID+":"+p.Digest] = plan
			}
		case EventHookExecutionStarted:
			id, err := DecodeHookExecutionStarted(record)
			if err != nil {
				return nil, err
			}
			if id.Event.Post() {
				index, ok := indices[tool.InvocationID(id.InvocationID())]
				if !ok || entries[index].PostHooks == nil {
					return nil, replayCorrupt("post hook has no plan projection")
				}
				g := entries[index].PostHooks
				g.Started = &id
				g.StartedSequence = record.Sequence
				entries[index].DetailSequence = record.Sequence
			}
		case EventToolPostHookCompleted:
			terminal, err := DecodeToolPostHookCompleted(record)
			if err != nil {
				return nil, err
			}
			index, ok := indices[tool.InvocationID(terminal.InvocationID)]
			if !ok || entries[index].PostHooks == nil {
				return nil, replayCorrupt("post hook has no plan projection")
			}
			entries[index].PostHooks.Terminal = &terminal
			entries[index].PostHooks.TerminalSequence = record.Sequence
			entries[index].DetailSequence = record.Sequence
		case EventHookExecutionFinished:
			finished, err := DecodeHookExecutionFinished(record)
			r := finished.Result
			if err != nil {
				return nil, err
			}
			if r.Identity.Event == domain.HookStop {
				continue
			}
			p, ok := plans[string(r.Identity.Event)+":"+r.Identity.InvocationID()+":"+r.Identity.PlanDigest]
			index, found := indices[tool.InvocationID(r.Identity.InvocationID())]
			if !ok || !found || r.Identity.Ordinal >= len(p.Hooks) {
				return nil, replayCorrupt("hook result has no plan projection")
			}
			entries[index].Hooks = append(entries[index].Hooks, HookHistoryEntry{Reference: p.Hooks[r.Identity.Ordinal], Result: r, Feedback: finished.Feedback})
			if r.Identity.Event.Post() {
				g := entries[index].PostHooks
				if g == nil {
					return nil, replayCorrupt("post hook has no plan projection")
				}
				g.Finished = append(g.Finished, finished)
				g.Started = nil
				g.StartedSequence = 0
				entries[index].DetailSequence = record.Sequence
			}
		case EventToolCallResult:
			p, err := DecodeToolCallResultPayload(record)
			if err != nil {
				return nil, err
			}
			index, ok := indices[p.InvocationID]
			if !ok || entries[index].HasResult {
				return nil, replayCorrupt("tool result has no ready fact")
			}
			result, err := p.Domain(entries[index].Invocation)
			if err != nil {
				return nil, err
			}
			entries[index].Result = result
			entries[index].HasResult = true
		}
	}
	return entries, nil
}

// ToolArtifactReferences 只投影已提交工具结果中的授权引用，不访问对象。
func ToolArtifactReferences(entries []ToolHistoryEntry) []tool.ArtifactDescriptor {
	var refs []tool.ArtifactDescriptor
	for _, entry := range entries {
		if !entry.HasResult {
			continue
		}
		refs = append(refs, entry.Result.ArtifactDescriptors()...)
	}
	return refs
}

// HookHistoryEntry 是可重建的安全检查详情，不包含可执行命令或输出正文。
type HookHistoryEntry struct {
	Reference domain.HookReference
	Result    domain.HookRunResult
	Feedback  domain.FrozenHookFeedback
}
