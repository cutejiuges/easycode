package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// CompactionSummarizingPayload 标记检查及最终预算验证后已接纳的摘要请求。
type CompactionSummarizingPayload struct {
	Scope domain.CompactionScope `json:"scope"`
}

// Validate 校验摘要阶段的真实压缩作用域。
func (payload CompactionSummarizingPayload) Validate() error { return payload.Scope.Validate() }

// NewCompactionSummarizing 创建仅用于交互宿主的摘要请求阶段通知。
func NewCompactionSummarizing(sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID, scope domain.CompactionScope) (Event, error) {
	event, err := newEventWithPayload(EventCompactionSummarizing, CompactionSummarizingPayload{Scope: scope})
	if err != nil {
		return Event{}, err
	}
	event.SessionID, event.ThreadID = sessionID, threadID
	event.CompactionID, event.TurnID = id, scope.TurnID
	return event, event.Validate()
}

// DecodeCompactionSummarizing 严格解码摘要阶段通知并核对作用域。
func DecodeCompactionSummarizing(event Event) (CompactionSummarizingPayload, error) {
	var payload CompactionSummarizingPayload
	if event.Kind != EventCompactionSummarizing || len(event.Payload) > 1<<10 || codec.UnmarshalRecord(event.Payload, &payload) != nil ||
		!event.CompactionID.Valid() || event.TurnID != payload.Scope.TurnID {
		return payload, fmt.Errorf("invalid compaction summary admission")
	}
	return payload, payload.Validate()
}

// IsCompactionPresentation 判断事件是否仅供本地交互宿主展示。
func IsCompactionPresentation(event Event) bool {
	switch event.Kind {
	case EventCompactionSummarizing:
		return true
	case EventHookTrustRequested, EventHookTrustResolved, EventHookActivity:
		return event.CompactionID != ""
	default:
		return false
	}
}

// CompactionEventScope 从同一压缩的生命周期或宿主通知中取得已验证的作用域。
func CompactionEventScope(event Event) (domain.CompactionScope, error) {
	if !event.CompactionID.Valid() {
		return domain.CompactionScope{}, fmt.Errorf("invalid compaction event identity")
	}
	switch event.Kind {
	case EventCompactionStarted:
		payload, err := DecodeCompactionStarted(event)
		return payload.Scope, err
	case EventCompactionCompleted:
		payload, err := DecodeCompactionCompleted(event)
		return payload.Scope, err
	case EventCompactionFailed:
		payload, err := DecodeCompactionFailed(event)
		return payload.Scope, err
	case EventCompactionSummarizing:
		payload, err := DecodeCompactionSummarizing(event)
		return payload.Scope, err
	case EventHookTrustRequested:
		payload, err := DecodeHookTrustRequested(event)
		_, scope, _ := payload.Target.Compaction()
		return scope, err
	case EventHookTrustResolved:
		payload, err := DecodeHookTrustResolved(event)
		_, scope, _ := payload.Target.Compaction()
		return scope, err
	case EventHookActivity:
		payload, err := DecodeHookActivity(event)
		_, scope, _ := payload.Target.Compaction()
		return scope, err
	default:
		return domain.CompactionScope{}, fmt.Errorf("invalid compaction event kind")
	}
}
