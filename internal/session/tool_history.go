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
}

func projectToolHistory(records []Record) ([]ToolHistoryEntry, error) {
	entries := make([]ToolHistoryEntry, 0)
	indices := make(map[tool.InvocationID]int)
	var nativeSequence uint64
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
