package runtime

import (
	"slices"

	"easycode/internal/tool"
)

const maxReadObservations = 1024

type pendingReadObservation struct {
	observation tool.ReadObservation
	generation  uint64
}

// readObservations 只由 turn owner 访问；worker 仅返回自己的固定结果槽。
type readObservations struct {
	current     map[string]tool.ReadObservation
	order       []string
	generations map[string]uint64
	pending     []pendingReadObservation
}

func (table *readObservations) beginGroup() {
	table.generations = make(map[string]uint64)
	table.pending = nil
}

func (table *readObservations) lookup(path string) tool.ReadObservation {
	if value, ok := table.current[path]; ok {
		return value
	}
	return tool.UnavailableReadObservation()
}

func (table *readObservations) invalidate(path string) {
	delete(table.current, path)
	if index := slices.Index(table.order, path); index >= 0 {
		table.order = slices.Delete(table.order, index, index+1)
	}
	if table.generations == nil {
		table.generations = make(map[string]uint64)
	}
	table.generations[path]++
}

func (table *readObservations) stage(result tool.InvocationResult, observation tool.ReadObservation) {
	if result.Capability() != tool.CapabilityRead {
		return
	}
	path := result.ReadMetadata().RelativePath()
	if observation.Kind() != tool.ReadObservationComplete {
		// 入口取消等失败没有可信规范路径，保守清空已有与待提交观察。
		if path == "." {
			table.clear()
			return
		}
		table.invalidate(path)
		return
	}
	table.pending = append(table.pending, pendingReadObservation{observation: observation, generation: table.generations[path]})
}

func (table *readObservations) activate() {
	if table.current == nil {
		table.current = make(map[string]tool.ReadObservation)
	}
	for _, pending := range table.pending {
		path := pending.observation.RelativePath()
		if table.generations[path] != pending.generation {
			continue
		}
		if index := slices.Index(table.order, path); index >= 0 {
			table.order = slices.Delete(table.order, index, index+1)
		}
		table.current[path] = pending.observation
		table.order = append(table.order, path)
		if len(table.order) > maxReadObservations {
			delete(table.current, table.order[0])
			table.order = table.order[1:]
		}
	}
	table.pending = nil
	table.generations = nil
}

func (table *readObservations) clear() { *table = readObservations{} }

// clearTurnObservations 由唯一 turn owner 在退出时清理未提交与 poisoned 证据。
func (runtime *Runtime) clearTurnObservations() {
	if runtime.poisoned.Load() || runtime.journal.Poisoned() {
		runtime.readObservations.clear()
		return
	}
	runtime.readObservations.pending = nil
	runtime.readObservations.generations = nil
}

// clearFileState 在会话 owner 完全停止后清空瞬态证据和能力授权。
func (runtime *Runtime) clearFileState() {
	runtime.readObservations.clear()
	runtime.writeGrant.Store(false)
	runtime.patchGrant.Store(false)
}
