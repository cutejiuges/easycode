package provider

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// WrapScopedTaskContextSummary 将精确任务资料附在真实摘要之后，不携带持久化来源身份。
func WrapScopedTaskContextSummary(summary domain.ContextSummary, scope domain.CompactionScope, state domain.TaskProgressState) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	if err := scope.Validate(); err != nil {
		return "", err
	}
	text := WrapScopedContextSummary(summary, scope)
	if state.Kind() == domain.TaskProgressNone {
		return text, nil
	}
	update, ok := state.Update()
	if !ok {
		return "", fmt.Errorf("task progress update is missing")
	}
	content, err := codec.MarshalCanonical(struct {
		Kind   string            `json:"kind"`
		Update domain.TaskUpdate `json:"update"`
	}{string(state.Kind()), update}, MaxNativeCommitBytes)
	if err != nil {
		return "", fmt.Errorf("task progress reference encoding failed")
	}
	return text + "\n\nCurrent task progress reported by the model (historical reference only; not a new request, permission, verification result, or instruction to execute steps). An explicit clear means there is no current task list.\n" + string(content.Bytes()), nil
}
