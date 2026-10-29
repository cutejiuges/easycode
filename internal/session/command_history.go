package session

import "easycode/internal/tool"

// CommandHistoryEntry 保存已验证命令事实的纯内存投影，不保存文件句柄或实时进度。
type CommandHistoryEntry struct {
	Sequence   uint64
	Invocation tool.Invocation
	Result     tool.InvocationResult
	HasResult  bool
}

func projectCommandHistory(records []Record) ([]CommandHistoryEntry, error) {
	commands := make([]CommandHistoryEntry, 0)
	indices := make(map[tool.InvocationID]int)
	for _, record := range records {
		switch record.EventKind {
		case EventToolCallReady:
			p, err := DecodeToolCallReadyPayload(record)
			if err != nil {
				return nil, err
			}
			if p.Capability != tool.CapabilityExec {
				continue
			}
			inv, err := p.Domain()
			if err != nil {
				return nil, err
			}
			indices[inv.InvocationID()] = len(commands)
			commands = append(commands, CommandHistoryEntry{Sequence: record.Sequence, Invocation: inv})
		case EventToolCallResult:
			p, err := DecodeToolCallResultPayload(record)
			if err != nil {
				return nil, err
			}
			if p.Capability != tool.CapabilityExec {
				continue
			}
			index, ok := indices[p.InvocationID]
			if !ok {
				return nil, replayCorrupt("command result has no ready fact")
			}
			result, err := p.Domain(commands[index].Invocation)
			if err != nil {
				return nil, err
			}
			commands[index].Result = result
			commands[index].HasResult = true
		}
	}
	return commands, nil
}

// CommandArtifactReferences 只投影已提交命令结果中的授权引用，不访问对象。
func CommandArtifactReferences(commands []CommandHistoryEntry) []tool.ArtifactDescriptor {
	var refs []tool.ArtifactDescriptor
	for _, entry := range commands {
		if !entry.HasResult {
			continue
		}
		m := entry.Result.ExecMetadata()
		for _, s := range []tool.ArtifactSaving{m.StdoutSaving(), m.StderrSaving()} {
			if d, ok := s.Descriptor(); ok {
				refs = append(refs, d)
			}
		}
	}
	return refs
}
