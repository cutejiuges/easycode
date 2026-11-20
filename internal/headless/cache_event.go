package headless

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// StreamCacheSnapshotEvent 是显式查询才产生的外部 v1 快照。
type StreamCacheSnapshotEvent struct {
	Version   int                 `json:"version"`
	Type      string              `json:"type"`
	RequestID protocol.RequestID  `json:"request_id"`
	SessionID domain.SessionID    `json:"session_id"`
	ThreadID  domain.ThreadID     `json:"thread_id"`
	Snapshot  StreamCacheSnapshot `json:"snapshot"`
}

// StreamCacheSnapshot 显式限定外部可见字段，不直接编码 Runtime 状态。
type StreamCacheSnapshot struct {
	Scope               string                            `json:"scope"`
	Baseline            string                            `json:"baseline"`
	Activity            string                            `json:"activity"`
	Family              domain.ProviderFamily             `json:"provider"`
	Wire                string                            `json:"wire"`
	Model               string                            `json:"model"`
	Policy              protocol.CachePolicyPayload       `json:"policy"`
	Sources             [6]protocol.CacheSourcePayload    `json:"sources"`
	StableFingerprint   string                            `json:"stable_fingerprint"`
	ContentFingerprint  string                            `json:"content_fingerprint"`
	ChangeReason        string                            `json:"change_reason"`
	ChangedSource       string                            `json:"changed_source"`
	PolicyChanged       bool                              `json:"policy_changed"`
	UnexplainedReadDrop bool                              `json:"unexplained_cache_read_drop"`
	LastFailure         string                            `json:"last_failure"`
	LastSample          *protocol.CacheObservationPayload `json:"last_sample,omitempty"`
	LastTurn            *protocol.UsagePayload            `json:"last_turn,omitempty"`
	LastSummary         *protocol.CacheObservationPayload `json:"last_summary,omitempty"`
}

func (StreamCacheSnapshotEvent) streamEvent() {}
func (p StreamCacheSnapshot) validate() error {
	return protocol.CacheSnapshotPayload(p).Validate()
}
