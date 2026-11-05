package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// CachePolicyPayload 只包含显式策略枚举，不包含真实 key 或连接地址。
type CachePolicyPayload struct {
	Anthropic   domain.AnthropicCacheMode  `json:"anthropic_prompt_cache"`
	Key         domain.OpenAICacheKeyMode  `json:"openai_prompt_cache_key"`
	Transport   domain.OpenAITransportMode `json:"openai_transport"`
	Fingerprint string                     `json:"fingerprint"`
}

// NewCachePolicyPayload 创建可复制的策略摘要。
func NewCachePolicyPayload(policy domain.CachePolicy) (CachePolicyPayload, error) {
	fingerprint, err := policy.Fingerprint()
	if err != nil {
		return CachePolicyPayload{}, err
	}
	return CachePolicyPayload{policy.Anthropic(), policy.Key(), policy.Transport(), fingerprint}, nil
}

// Domain 严格验证并重建纯策略值。
func (p CachePolicyPayload) Domain() (domain.CachePolicy, error) {
	value, err := domain.NewCachePolicy(p.Anthropic, p.Key, p.Transport)
	if err != nil {
		return domain.CachePolicy{}, err
	}
	fingerprint, err := value.Fingerprint()
	if err != nil {
		return domain.CachePolicy{}, err
	}
	if p.Fingerprint != fingerprint {
		return domain.CachePolicy{}, fmt.Errorf("cache policy fingerprint is invalid")
	}
	return value, nil
}

// CacheSourcePayload 是固定分类、无正文的来源摘要。
type CacheSourcePayload struct {
	Kind        string `json:"kind"`
	Present     bool   `json:"present"`
	Revision    string `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}

// CacheObservationPayload 保存一次已经 durable 的普通或摘要观测。
type CacheObservationPayload struct {
	Purpose            domain.RequestPurpose      `json:"purpose"`
	CompactionID       domain.CompactionID        `json:"compaction_id,omitempty"`
	Usage              UsagePayload               `json:"usage"`
	ReadRatio          *float64                   `json:"read_ratio,omitempty"`
	ReadObserved       bool                       `json:"read_observed"`
	RequestFingerprint string                     `json:"request_fingerprint"`
	NativeFingerprint  string                     `json:"native_fingerprint"`
	Transport          domain.RequestTransport    `json:"transport"`
	Mode               domain.RequestMode         `json:"mode"`
	Relation           domain.NativeRelation      `json:"relation"`
	Reason             domain.RequestResetReason  `json:"reason"`
	Fallback           domain.CacheFallbackReason `json:"fallback"`
	Markers            uint8                      `json:"markers"`
}

// CacheSnapshotPayload 是最大 64 KiB 的进程级查询契约。
type CacheSnapshotPayload struct {
	Scope               string                   `json:"scope"`
	Baseline            string                   `json:"baseline"`
	Activity            string                   `json:"activity"`
	Family              domain.ProviderFamily    `json:"provider"`
	Wire                string                   `json:"wire"`
	Model               string                   `json:"model"`
	Policy              CachePolicyPayload       `json:"policy"`
	Sources             [6]CacheSourcePayload    `json:"sources"`
	StableFingerprint   string                   `json:"stable_fingerprint"`
	ContentFingerprint  string                   `json:"content_fingerprint"`
	ChangeReason        string                   `json:"change_reason"`
	ChangedSource       string                   `json:"changed_source"`
	PolicyChanged       bool                     `json:"policy_changed"`
	UnexplainedReadDrop bool                     `json:"unexplained_cache_read_drop"`
	LastFailure         string                   `json:"last_failure"`
	LastSample          *CacheObservationPayload `json:"last_sample,omitempty"`
	LastTurn            *UsagePayload            `json:"last_turn,omitempty"`
	LastSummary         *CacheObservationPayload `json:"last_summary,omitempty"`
}

// Validate 拒绝无界元数据、未知状态与矛盾计量。
func (p CacheSnapshotPayload) Validate() error {
	if p.Scope != "process" || !p.Family.Valid() || p.Model == "" || p.Model != CacheModelIdentity(p.Model) {
		return fmt.Errorf("cache snapshot scope or model is invalid")
	}

	if (p.Family == domain.ProviderOpenAI && p.Wire != "responses") || (p.Family == domain.ProviderAnthropic && p.Wire != "messages") {
		return fmt.Errorf("cache wire is invalid")
	}
	policy, err := p.Policy.Domain()
	if err != nil {
		return err
	}
	if err = policy.ValidateFamily(p.Family); err != nil {
		return err
	}
	if p.Baseline != "fresh" && p.Baseline != "restored_baseline" {
		return fmt.Errorf("cache baseline is invalid")
	}
	if p.Activity != "idle" && p.Activity != "turn" && p.Activity != "compacting" {
		return fmt.Errorf("cache activity is invalid")
	}
	order := [6]string{"provider_profile", "tool_catalog", "project_instructions", "skill_catalog", "committed_history", "current_input"}
	for i, s := range p.Sources {
		if s.Kind != order[i] {
			return fmt.Errorf("cache source order is invalid")
		}
		if !s.Present {
			if (i != 2 && i != 3) || s.Revision != "" || s.Fingerprint != "" {
				return fmt.Errorf("cache source is missing")
			}
			continue
		}
		if len(s.Revision) == 0 || len(s.Revision) > 128 || !domain.ValidFingerprint(s.Fingerprint) {
			return fmt.Errorf("cache source metadata is invalid")
		}
		for _, r := range s.Revision {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return fmt.Errorf("cache revision is invalid")
			}
		}
	}
	if !domain.ValidFingerprint(p.StableFingerprint) || !domain.ValidFingerprint(p.ContentFingerprint) {
		return fmt.Errorf("cache content fingerprint is invalid")
	}
	switch p.ChangeReason {
	case "first_request", "restored_baseline", "unchanged", "history_appended", "source_content_changed", "source_revision_changed", "source_added", "source_removed", "native_context_changed", "context_compacted":
	default:
		return fmt.Errorf("cache change reason is invalid")
	}
	if p.ChangedSource != "" {
		found := false
		for _, kind := range order {
			found = found || p.ChangedSource == kind
		}
		if !found {
			return fmt.Errorf("changed cache source is invalid")
		}
	}
	switch p.LastFailure {
	case "", "turn_failed", "compaction_failed", "cancelled":
	default:
		return fmt.Errorf("cache failure is invalid")
	}
	for _, entry := range []struct {
		value   *CacheObservationPayload
		purpose domain.RequestPurpose
	}{{p.LastSample, domain.RequestSample}, {p.LastSummary, domain.RequestSummary}} {
		o := entry.value
		if o == nil {
			continue
		}
		if o.Purpose != entry.purpose || (o.Purpose == domain.RequestSummary && !o.CompactionID.Valid()) || (o.Purpose == domain.RequestSample && o.CompactionID != "") {
			return fmt.Errorf("cache observation scope is invalid")
		}
		usage, err := o.Usage.Domain()
		if err != nil {
			return err
		}
		read, known := usage.CacheRead().Value()
		if o.ReadObserved != (known && read > 0) {
			return fmt.Errorf("cache read evidence is invalid")
		}
		ratio, ratioKnown := domain.CacheReadRatio(usage)
		if (o.ReadRatio != nil) != ratioKnown || (ratioKnown && *o.ReadRatio != ratio) {
			return fmt.Errorf("cache read ratio does not match usage")
		}
		if o.ReadRatio != nil && (math.IsNaN(*o.ReadRatio) || math.IsInf(*o.ReadRatio, 0) || *o.ReadRatio < 0 || *o.ReadRatio > 1) {
			return fmt.Errorf("cache read ratio is invalid")
		}
		if _, err := domain.NewRequestReceipt(domain.RequestReceipt{Family: p.Family, Purpose: o.Purpose, Policy: policy, RequestFingerprint: o.RequestFingerprint, NativeFingerprint: o.NativeFingerprint, Relation: o.Relation, Transport: o.Transport, Mode: o.Mode, Reason: o.Reason, Fallback: o.Fallback, Markers: o.Markers}); err != nil {
			return err
		}
	}
	if p.LastTurn != nil {
		if _, err := p.LastTurn.Domain(); err != nil {
			return err
		}
	}
	raw, err := codec.MarshalStable(p)
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return fmt.Errorf("cache snapshot exceeds maximum size")
	}
	return nil
}

// NewCacheSnapshot 创建只读 thread 查询事件。
func NewCacheSnapshot(sessionID domain.SessionID, threadID domain.ThreadID, payload CacheSnapshotPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, err
	}
	event := newEvent(EventCacheSnapshot)
	event.SessionID = sessionID
	event.ThreadID = threadID
	event.Payload = raw
	return event, event.Validate()
}

// DecodeCacheSnapshot 严格解码有界的当前缓存快照。
func DecodeCacheSnapshot(event Event) (CacheSnapshotPayload, error) {
	var payload CacheSnapshotPayload
	if event.Kind != EventCacheSnapshot || len(event.Payload) > 64<<10 {
		return payload, fmt.Errorf("cache snapshot event is invalid")
	}
	var shape struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if err := codec.Unmarshal(event.Payload, &shape); err != nil || len(shape.Sources) != 6 {
		return payload, fmt.Errorf("cache snapshot source count is invalid")
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	return payload, payload.Validate()
}

// NewQueryCacheCommand 创建不分配输入或 turn 身份的只读命令。
func NewQueryCacheCommand(requestID RequestID) (Command, error) {
	command := Command{kind: CommandQueryCache, requestID: requestID}
	return command, command.Validate()
}

// CacheModelIdentity 将不安全或无界模型标签转换为不可逆身份，不改变真实请求模型。
func CacheModelIdentity(model string) string {
	unsafe := len(model) > 512 || !utf8.ValidString(model) || strings.HasPrefix(model, "/") || strings.HasPrefix(model, "\\")
	if len(model) >= 3 && ((model[0] >= 'a' && model[0] <= 'z') || (model[0] >= 'A' && model[0] <= 'Z')) && model[1] == ':' && (model[2] == '/' || model[2] == '\\') {
		unsafe = true
	}
	for _, r := range model {
		if unicode.IsControl(r) {
			unsafe = true
			break
		}
	}
	if unsafe {
		return "sha256:" + domain.ContentFingerprint([]byte(model))
	}
	return model
}
