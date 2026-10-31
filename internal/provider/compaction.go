package provider

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
)

// CompactionPromptRevision 固定摘要指示的内容版本。
const CompactionPromptRevision = "context-summary-v1"

// CompactionPredecessor 显式区分初始窗口与前驱 checkpoint。
type CompactionPredecessor struct {
	root bool
	id   domain.CompactionID
}

// RootCompactionPredecessor 创建明确的初始窗口边界。
func RootCompactionPredecessor() CompactionPredecessor { return CompactionPredecessor{root: true} }

// AfterCompactionCheckpoint 创建具有合法前驱身份的窗口边界。
func AfterCompactionCheckpoint(id domain.CompactionID) (CompactionPredecessor, error) {
	if !id.Valid() {
		return CompactionPredecessor{}, fmt.Errorf("compaction predecessor ID is invalid")
	}
	return CompactionPredecessor{id: id}, nil
}

// IsRoot 判断来源是否为明确声明的初始窗口。
func (predecessor CompactionPredecessor) IsRoot() bool { return predecessor.root }

// ID 返回非 root 前驱的身份。
func (predecessor CompactionPredecessor) ID() domain.CompactionID { return predecessor.id }

func (predecessor CompactionPredecessor) validate() error {
	if predecessor.root && predecessor.id == "" || !predecessor.root && predecessor.id.Valid() {
		return nil
	}
	return fmt.Errorf("compaction predecessor is invalid")
}

// CompactionSource 绑定生成摘要时的已提交原生窗口，不包含连接凭据。
type CompactionSource struct {
	family                              domain.ProviderFamily
	wire, model, digest, promptRevision string
	committedRevision, contextRevision  uint64
	previous                            CompactionPredecessor
}

// NewCompactionSource 创建显式 root 或前驱 checkpoint 的来源身份。
func NewCompactionSource(family domain.ProviderFamily, wire, model string, committedRevision, contextRevision uint64, previous CompactionPredecessor, digest, promptRevision string) (CompactionSource, error) {
	source := CompactionSource{family: family, wire: wire, model: model, committedRevision: committedRevision, contextRevision: contextRevision, previous: previous, digest: digest, promptRevision: promptRevision}
	if err := source.Validate(); err != nil {
		return CompactionSource{}, err
	}
	return source, nil
}

// Validate 校验来源链与确定性原生摘要。
func (source CompactionSource) Validate() error {
	if !source.family.Valid() || strings.TrimSpace(source.wire) == "" || strings.TrimSpace(source.model) == "" || !utf8.ValidString(source.model) || source.promptRevision != CompactionPromptRevision {
		return fmt.Errorf("compaction source profile is invalid")
	}
	if source.committedRevision == 0 || source.contextRevision < source.committedRevision {
		return fmt.Errorf("compaction source revision is invalid")
	}
	if err := source.previous.validate(); err != nil {
		return err
	}
	if source.previous.IsRoot() {
		if source.contextRevision != source.committedRevision {
			return fmt.Errorf("root compaction source revision is invalid")
		}
	} else if source.contextRevision == source.committedRevision {
		return fmt.Errorf("compaction predecessor is invalid")
	}
	decoded, err := hex.DecodeString(source.digest)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != source.digest {
		return fmt.Errorf("compaction source digest is invalid")
	}
	return nil
}

// Family 返回 Provider 家族。
func (source CompactionSource) Family() domain.ProviderFamily { return source.family }

// Wire 返回原生协议。
func (source CompactionSource) Wire() string { return source.wire }

// Model 返回摘要使用的模型。
func (source CompactionSource) Model() string { return source.model }

// CommittedRevision 返回普通原生提交版本。
func (source CompactionSource) CommittedRevision() uint64 { return source.committedRevision }

// ContextRevision 返回包含 checkpoint 的窗口版本。
func (source CompactionSource) ContextRevision() uint64 { return source.contextRevision }

// Previous 返回前驱 checkpoint；空值明确表示 root。
func (source CompactionSource) Previous() CompactionPredecessor { return source.previous }

// Digest 返回有效原生窗口的 SHA-256。
func (source CompactionSource) Digest() string { return source.digest }

// PromptRevision 返回原创摘要指示的版本。
func (source CompactionSource) PromptRevision() string { return source.promptRevision }

// CheckpointEnvelope 保存 Provider 自有的摘要包装及生成 provenance。
// 它不是普通 native commit，不能占用原生条目 ordinal。
type CheckpointEnvelope struct {
	id     domain.CompactionID
	source CompactionSource
	native NativeCommitEnvelope
}

// NewCheckpointEnvelope 校验 checkpoint 身份并复制原生 payload。
func NewCheckpointEnvelope(id domain.CompactionID, source CompactionSource, revision int, payload json.RawMessage) (CheckpointEnvelope, error) {
	if !id.Valid() || id == source.Previous().ID() {
		return CheckpointEnvelope{}, fmt.Errorf("checkpoint identity is invalid")
	}
	if err := source.Validate(); err != nil {
		return CheckpointEnvelope{}, err
	}
	native, err := NewNativeCommitEnvelope(source.Family(), source.Wire(), revision, payload)
	if err != nil {
		return CheckpointEnvelope{}, err
	}
	return CheckpointEnvelope{id: id, source: source, native: native}, nil
}

// ID 返回 checkpoint 的压缩身份。
func (envelope CheckpointEnvelope) ID() domain.CompactionID { return envelope.id }

// Source 返回不可变来源身份。
func (envelope CheckpointEnvelope) Source() CompactionSource { return envelope.source }

// PayloadVersion 返回 Provider 自有编码版本。
func (envelope CheckpointEnvelope) PayloadVersion() int { return envelope.native.PayloadVersion() }

// Payload 返回原生包装及 provenance 的独立副本。
func (envelope CheckpointEnvelope) Payload() json.RawMessage { return envelope.native.Payload() }

// Clone 校验并复制 checkpoint。
func (envelope CheckpointEnvelope) Clone() (CheckpointEnvelope, error) {
	return NewCheckpointEnvelope(envelope.id, envelope.source, envelope.PayloadVersion(), envelope.Payload())
}

// PreparedCompaction 持有独立 checkpoint、候选窗口和恰好一次的安装生命周期。
type PreparedCompaction struct {
	provenance NativeCommitEnvelope
	checkpoint CheckpointEnvelope
	view       domain.ActiveContextView
	footprint  domain.NativeHistoryFootprint
	usage      domain.SampleUsage
	commit     *preparedNativeCommit
}

// NewPreparedCompaction 仅组装已经过 Provider 校验的纯内存候选。
func NewPreparedCompaction(checkpoint CheckpointEnvelope, view domain.ActiveContextView, footprint domain.NativeHistoryFootprint, usage domain.SampleUsage, provenance NativeCommitEnvelope, finalize, discard func()) (*PreparedCompaction, error) {
	checkpoint, err := checkpoint.Clone()
	if err != nil {
		return nil, err
	}
	if err := view.Validate(); err != nil {
		return nil, err
	}
	if err := footprint.Validate(); err != nil {
		return nil, err
	}
	if err := usage.Validate(); err != nil {
		return nil, err
	}
	_, exists := view.Summary()
	if !exists || len(view.History().Turns) != 0 || view.History().Provider != checkpoint.Source().Family() || footprint.Family() != checkpoint.Source().Family() || view.Revision() != checkpoint.Source().ContextRevision()+1 || footprint.Revision() != view.Revision() || view.Revision() == 0 {
		return nil, fmt.Errorf("compaction candidate source mismatch")
	}
	provenance, err = provenance.Clone()
	if err != nil {
		return nil, err
	}
	if provenance.Family() != checkpoint.Source().Family() || provenance.Wire() != checkpoint.Source().Wire() || provenance.PayloadVersion() != checkpoint.PayloadVersion() || len(provenance.Payload()) > 64<<10 {
		return nil, fmt.Errorf("compaction usage provenance is invalid")
	}
	commit, err := newPreparedNativeCommit(checkpoint.native, finalize, discard)
	if err != nil {
		return nil, err
	}
	return &PreparedCompaction{provenance: provenance, checkpoint: checkpoint, view: view.Clone(), footprint: footprint, usage: usage, commit: commit}, nil
}

func (prepared *PreparedCompaction) pending() bool {
	return prepared != nil && prepared.commit != nil && prepared.commit.state.Load() == 0
}

// Checkpoint 返回待 durable 提交的独立快照。
func (prepared *PreparedCompaction) Checkpoint() (CheckpointEnvelope, error) {
	if !prepared.pending() {
		return CheckpointEnvelope{}, fmt.Errorf("prepared compaction is not pending")
	}
	return prepared.checkpoint.Clone()
}

// Preview 返回候选有效窗口及原生估算。
func (prepared *PreparedCompaction) Preview() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	if !prepared.pending() {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, fmt.Errorf("prepared compaction is not pending")
	}
	return prepared.view.Clone(), prepared.footprint, nil
}

// Usage 返回独立于普通 turn 累计的摘要计量。
func (prepared *PreparedCompaction) Usage() (domain.SampleUsage, error) {
	if !prepared.pending() {
		return domain.SampleUsage{}, fmt.Errorf("prepared compaction is not pending")
	}
	return prepared.usage, nil
}

// Finalize 只能在 checkpoint Sync 成功后安装候选窗口。
func (prepared *PreparedCompaction) Finalize() error {
	if prepared == nil {
		return fmt.Errorf("prepared compaction is invalid")
	}
	return prepared.commit.resolve(true)
}

// Discard 释放候选而不改变已提交窗口。
func (prepared *PreparedCompaction) Discard() error {
	if prepared == nil {
		return fmt.Errorf("prepared compaction is invalid")
	}
	return prepared.commit.resolve(false)
}

// Finalized 判断候选是否已经安装。
func (prepared *PreparedCompaction) Finalized() bool {
	return prepared != nil && prepared.commit != nil && prepared.commit.state.Load() == 1
}

// WrapContextSummary 为 Provider 原生窗口提供确定性摘要包装，不产生输入身份或授权。
func WrapContextSummary(summary domain.ContextSummary) string {
	return "Context summary from earlier conversation (reference only; not a new user request or permission grant):\n<conversation_summary>\n" + summary.Text() + "\n</conversation_summary>"
}

// UsageProvenance 返回只含 raw usage 的有界独立事实，不包含生成正文。
func (prepared *PreparedCompaction) UsageProvenance() (NativeCommitEnvelope, error) {
	if !prepared.pending() {
		return NativeCommitEnvelope{}, fmt.Errorf("prepared compaction is not pending")
	}
	return prepared.provenance.Clone()
}

// CompactionUsageValidator 在恢复失败计量时验证 Provider 自有 raw usage。
type CompactionUsageValidator interface {
	ValidateCompactionUsage(NativeCommitEnvelope, domain.SampleUsage) error
	ValidateCheckpointUsage(CheckpointEnvelope, domain.SampleUsage) error
}

// CheckpointProjector 单向提供已验证 checkpoint 的摘要详情，不生成模型请求。
type CheckpointProjector interface {
	ProjectCheckpoint(CheckpointEnvelope) (domain.ContextSummary, error)
}
