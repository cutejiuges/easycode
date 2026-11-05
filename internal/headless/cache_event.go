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
	Provider            domain.ProviderFamily             `json:"provider"`
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
func newStreamCacheSnapshot(p protocol.CacheSnapshotPayload) StreamCacheSnapshot {
	return StreamCacheSnapshot{Scope: p.Scope, Baseline: p.Baseline, Activity: p.Activity, Provider: p.Family, Wire: p.Wire, Model: p.Model, Policy: p.Policy, Sources: p.Sources, StableFingerprint: p.StableFingerprint, ContentFingerprint: p.ContentFingerprint, ChangeReason: p.ChangeReason, ChangedSource: p.ChangedSource, PolicyChanged: p.PolicyChanged, UnexplainedReadDrop: p.UnexplainedReadDrop, LastFailure: p.LastFailure, LastSample: p.LastSample, LastTurn: p.LastTurn, LastSummary: p.LastSummary}
}
func (p StreamCacheSnapshot) validate() error {
	return (protocol.CacheSnapshotPayload{Scope: p.Scope, Baseline: p.Baseline, Activity: p.Activity, Family: p.Provider, Wire: p.Wire, Model: p.Model, Policy: p.Policy, Sources: p.Sources, StableFingerprint: p.StableFingerprint, ContentFingerprint: p.ContentFingerprint, ChangeReason: p.ChangeReason, ChangedSource: p.ChangedSource, PolicyChanged: p.PolicyChanged, UnexplainedReadDrop: p.UnexplainedReadDrop, LastFailure: p.LastFailure, LastSample: p.LastSample, LastTurn: p.LastTurn, LastSummary: p.LastSummary}).Validate()
}
