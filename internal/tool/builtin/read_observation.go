package builtin

import "easycode/internal/tool"

// readFileState 包含同一 fd 读取前后用于一致性比较的事实，不进入持久化。
type readFileState struct {
	device   uint64
	inode    uint64
	mode     uint32
	size     int64
	modified [4]int64
}

func observeRead(result tool.InvocationResult, before, after readFileState, data []byte) tool.ReadObservation {
	if before != after || after.size != int64(len(data)) {
		return tool.UnavailableReadObservation()
	}
	evidence, err := tool.NewFileBaseEvidence(tool.NewContentDigest(data), after.size, after.mode, after.device, after.inode)
	if err != nil {
		return tool.UnavailableReadObservation()
	}
	observation, err := tool.NewCompleteReadObservation(result, evidence)
	if err != nil {
		return tool.UnavailableReadObservation()
	}
	return observation
}
