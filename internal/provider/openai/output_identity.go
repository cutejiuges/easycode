package openai

import "easycode/internal/fault"

type outputIdentity struct {
	kind  string
	id    string
	index int
	done  bool
}

func knownOutputKind(kind string) bool {
	switch kind {
	case "message", "reasoning", "function_call", "custom_tool_call":
		return true
	default:
		return false
	}
}

// observeOutputIdentity 关联已知 item 的原生 ID 和位置，允许缺少元数据的 done-only 输出。
func (reducer *responsesStreamReducer) observeOutputIdentity(item NativeItem, index *int, done bool) error {
	if !knownOutputKind(item.Type) {
		return nil
	}
	position := -1
	if index != nil {
		position = *index
		if position < 0 {
			return fault.New(fault.CodeStreamProtocol, "Responses output item index is invalid")
		}
	}
	if item.ID == "" && position < 0 {
		if !done {
			return fault.New(fault.CodeStreamProtocol, "Responses output item identity is required")
		}
		return nil
	}
	byID, byIndex := reducer.outputIDs[item.ID], reducer.outputIndexes[position]
	identity := byID
	if identity == nil {
		identity = byIndex
	}
	if identity != nil {
		if !done || identity.done || identity.kind != item.Type ||
			byID != nil && byIndex != nil && byID != byIndex ||
			item.ID != "" && identity.id != "" && item.ID != identity.id ||
			position >= 0 && identity.index >= 0 && position != identity.index {
			return fault.New(fault.CodeStreamProtocol, "Responses output item identity conflicts")
		}
		if item.Type == "message" {
			reducer.pendingMessages--
		}
	} else {
		identity = &outputIdentity{kind: item.Type, index: -1}
		if item.Type == "message" && !done {
			reducer.pendingMessages++
		}
	}
	if item.ID != "" {
		identity.id = item.ID
		reducer.outputIDs[item.ID] = identity
	}
	if position >= 0 {
		identity.index = position
		reducer.outputIndexes[position] = identity
	}
	identity.done = done
	return nil
}
