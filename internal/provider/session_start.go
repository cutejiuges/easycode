package provider

import (
	"fmt"
	"math"
	"slices"

	"easycode/internal/domain"
)

// SessionStartPreparer 使用同一原生操作准备激活输入或已安装 checkpoint 的补充。
type SessionStartPreparer interface {
	PrepareSessionStartInputs(domain.SessionStartContext, domain.InputAssociation, []domain.UserInput) (*PreparedSessionStartContext, error)
	PrepareCompactSessionStartContext(domain.SessionStartContext, SessionStartContinuation) (*PreparedSessionStartContext, error)
}

// SessionStartContinuation 保留自动压缩前已冻结的下一普通候选，零值表示无额外输入。
type SessionStartContinuation struct {
	association domain.InputAssociation
	inputs      []domain.UserInput
	stop        domain.StopContinuationInput
}

// NewSessionStartInputContinuation 保存真实初始输入或工具后的 steering。
func NewSessionStartInputContinuation(association domain.InputAssociation, inputs []domain.UserInput) (SessionStartContinuation, error) {
	if err := association.Validate(); err != nil {
		return SessionStartContinuation{}, err
	}
	return SessionStartContinuation{association: association, inputs: slices.Clone(inputs)}, nil
}

// NewSessionStartStopContinuation 保存已冻结的 Stop 反馈及适用的 steering。
func NewSessionStartStopContinuation(stop domain.StopContinuationInput) (SessionStartContinuation, error) {
	if err := stop.Validate(); err != nil {
		return SessionStartContinuation{}, err
	}
	return SessionStartContinuation{stop: stop.Clone()}, nil
}

// PreparedSessionStartContext 为已授权上下文及适用真实输入保留一次提交或丢弃。
type PreparedSessionStartContext struct {
	context      *preparedNativeCommit
	input        NativeCommitEnvelope
	committed    InputPreview
	continuation InputPreview
}

// Envelope 返回已冻结的 Provider 原生上下文信封。
func (p *PreparedSessionStartContext) Envelope() (NativeCommitEnvelope, error) {
	if p == nil {
		return NativeCommitEnvelope{}, fmt.Errorf("session start context is invalid")
	}
	return p.context.envelopeCopy()
}

// InputEnvelope 返回激活时同批保存的真实输入信封。
func (p *PreparedSessionStartContext) InputEnvelope() (NativeCommitEnvelope, error) {
	if !p.pending() {
		return NativeCommitEnvelope{}, fmt.Errorf("session start context is not pending")
	}
	return p.input.Clone()
}

func (p *PreparedSessionStartContext) pending() bool {
	return p != nil && p.context != nil && p.context.resolution.pending()
}

// Preview 返回此次提交将发布的实际窗口。
func (p *PreparedSessionStartContext) Preview() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	if !p.pending() {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, fmt.Errorf("session start context is not pending")
	}
	return p.committed.history.Clone(), p.committed.footprint, nil
}

// ContinuationPreview 返回同时包括已知后续候选的窗口，后续候选仍由原调用方提交。
func (p *PreparedSessionStartContext) ContinuationPreview() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	if !p.pending() {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, fmt.Errorf("session start context is not pending")
	}
	return p.continuation.history.Clone(), p.continuation.footprint, nil
}

// CheckRequest 使用实际后续窗口编译请求并检查编码。
func (p *PreparedSessionStartContext) CheckRequest(input TurnInput) error {
	if !p.pending() {
		return fmt.Errorf("session start context is not pending")
	}
	return p.continuation.checkRequest(input)
}

// Finalize 仅在事实 Sync 后发布原生历史。
func (p *PreparedSessionStartContext) Finalize() error {
	if p == nil {
		return fmt.Errorf("session start context is invalid")
	}
	return p.context.resolve(true)
}

// Discard 释放未提交的候选。
func (p *PreparedSessionStartContext) Discard() error {
	if p == nil {
		return fmt.Errorf("session start context is invalid")
	}
	return p.context.resolve(false)
}

// Finalized 报告已完成的原生提交。
func (p *PreparedSessionStartContext) Finalized() bool {
	return p != nil && p.context != nil && p.context.resolution.finalized()
}

// SessionStartMetadata 将规范模型块绑定到作用域及各来源证据，不重复保存正文。
type SessionStartMetadata struct {
	Target domain.SessionStartTarget  `json:"target"`
	Proofs []domain.SessionStartProof `json:"proofs"`
}

// NewSessionStartMetadata 验证并保存每个提交块的来源和摘要。
func NewSessionStartMetadata(value domain.SessionStartContext) (SessionStartMetadata, error) {
	if err := value.Validate(); err != nil {
		return SessionStartMetadata{}, err
	}
	proofs := make([]domain.SessionStartProof, len(value.Items))
	for i, item := range value.Items {
		proofs[i] = item.Proof()
	}
	return SessionStartMetadata{Target: value.Target, Proofs: proofs}, nil
}

// Clone 隔离可变证明列表。
func (m SessionStartMetadata) Clone() SessionStartMetadata {
	m.Proofs = slices.Clone(m.Proofs)
	return m
}

// Context 用已验证的原生文本逐项核对元数据。
func (m SessionStartMetadata) Context(texts []string) (domain.SessionStartContext, error) {
	if len(texts) != len(m.Proofs) {
		return domain.SessionStartContext{}, fmt.Errorf("session start native context count differs from evidence")
	}
	value := domain.SessionStartContext{Target: m.Target, Items: make([]domain.SessionStartContextItem, len(texts))}
	for i, proof := range m.Proofs {
		item := domain.SessionStartContextItem{Ordinal: proof.Ordinal, Reference: proof.Reference, Text: texts[i]}
		if item.Proof() != proof {
			return domain.SessionStartContext{}, fmt.Errorf("session start native context differs from evidence")
		}
		value.Items[i] = item
	}
	return value, value.Validate()
}

// PrepareSessionStartInputs 一次保留上下文和真实初始输入的两个原生位置。
func (p NativeInputPreparation[E]) PrepareSessionStartInputs(value domain.SessionStartContext, association domain.InputAssociation, inputs []domain.UserInput) (*PreparedSessionStartContext, error) {
	if value.Target.Source == domain.SessionStartCompact || association.Placement() != domain.InputInitial || value.Target.TurnID != association.TurnID() {
		return nil, fmt.Errorf("session start context requires matching initial inputs")
	}
	next, err := NewSessionStartInputContinuation(association, inputs)
	if err != nil {
		return nil, err
	}
	return p.prepareSessionStart(value, next, true)
}

// PrepareCompactSessionStartContext 为同一次 checkpoint 追加独立原生上下文。
func (p NativeInputPreparation[E]) PrepareCompactSessionStartContext(value domain.SessionStartContext, next SessionStartContinuation) (*PreparedSessionStartContext, error) {
	if value.Target.Source != domain.SessionStartCompact {
		return nil, fmt.Errorf("session start context requires a compaction target")
	}
	if !next.matchesCompactionScope(value.Target.Scope) {
		return nil, fmt.Errorf("session start continuation does not match the compaction scope")
	}
	return p.prepareSessionStart(value, next, false)
}

func (next SessionStartContinuation) matchesCompactionScope(scope domain.CompactionScope) bool {
	if next.stop.Kind() != "" {
		sample, _ := next.stop.Feedback().Identity.Target.AssistantSample()
		return scope.Trigger == domain.CompactionAuto && scope.TurnID == sample.TurnID &&
			scope.SampleIndex > 0 && scope.SampleIndex-1 == sample.SampleIndex
	}
	if next.association.TurnID() != "" {
		return scope.Trigger == domain.CompactionAuto && scope.TurnID == next.association.TurnID() &&
			scope.SampleIndex == next.association.SampleIndex()
	}
	return true
}

func (p NativeInputPreparation[E]) prepareSessionStart(value domain.SessionStartContext, next SessionStartContinuation, initial bool) (*PreparedSessionStartContext, error) {
	return prepareNativeCommit(p.Operations, "conversation is active", "conversation has an unresolved commit", func() (*PreparedSessionStartContext, error) {
		contextEntry, err := p.SessionStart(value)
		if err != nil {
			return nil, err
		}
		window := p.History.Snapshot()
		if !initial && !matchesSessionStartCheckpoint(window, value.Target) {
			return nil, fmt.Errorf("session start context requires the installed checkpoint")
		}
		nextEntries, err := p.sessionStartContinuationEntries(next)
		if err != nil {
			return nil, err
		}
		entries := []E{contextEntry}
		var input NativeCommitEnvelope
		if initial {
			entries = append(entries, nextEntries...)
			input, err = p.Encode(nextEntries[0])
			if err != nil {
				return nil, err
			}
		}
		if err := p.appendSessionStartCandidate(&window, entries); err != nil {
			return nil, err
		}
		committed, err := p.preview(window)
		if err != nil {
			return nil, err
		}
		continuation := committed
		if !initial && len(nextEntries) != 0 {
			if err := p.appendSessionStartCandidate(&window, nextEntries); err != nil {
				return nil, err
			}
			continuation, err = p.preview(window)
			if err != nil {
				return nil, err
			}
		}
		envelope, err := p.Encode(contextEntry)
		if err != nil {
			return nil, err
		}
		commit, err := newPreparedNativeCommit(envelope, func() {
			p.History.AppendBatch(entries)
			p.Operations.ResolvePrepared()
		}, p.Operations.ResolvePrepared)
		if err != nil {
			return nil, err
		}
		return &PreparedSessionStartContext{context: commit, input: input, committed: committed, continuation: continuation}, nil
	})
}

func (p NativeInputPreparation[E]) appendSessionStartCandidate(window *NativeWindow[E], entries []E) error {
	if window.Revision > math.MaxUint64-uint64(len(entries)) {
		return fmt.Errorf("native history revision overflow")
	}
	window.Entries = append(window.Entries, entries...)
	window.Revision += uint64(len(entries))
	return p.Validate(window.Entries)
}

func (p NativeInputPreparation[E]) sessionStartContinuationEntries(next SessionStartContinuation) ([]E, error) {
	var entries []E
	association, inputs := next.association, next.inputs
	if next.stop.Kind() != "" {
		feedback, err := p.Feedback(next.stop.Feedback())
		if err != nil {
			return nil, err
		}
		entries = append(entries, feedback)
		association, _ = next.stop.Association()
		inputs = next.stop.Inputs()
	}
	if association.TurnID() != "" {
		inputEntry, err := p.UserEntry(association, inputs)
		if err != nil {
			return nil, err
		}
		entries = append(entries, inputEntry)
	}
	return entries, nil
}

func matchesSessionStartCheckpoint[E nativeClone[E]](window NativeWindow[E], target domain.SessionStartTarget) bool {
	checkpoint := window.Checkpoint
	return checkpoint != nil && window.Offset == len(window.Entries) && checkpoint.ID() == target.CompactionID &&
		checkpoint.Scope() == target.Scope && window.Revision <= math.MaxUint64-window.Count &&
		target.CheckpointRevision == window.Revision+window.Count && checkpoint.Source().Digest() == target.CheckpointDigest
}
