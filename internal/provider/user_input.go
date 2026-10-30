package provider

import (
	"fmt"
	"sync/atomic"

	"easycode/internal/domain"
)

// UserInputPreparer 纯内存准备独立用户输入，不建立网络连接或改变已提交历史。
type UserInputPreparer interface {
	PrepareUserInputs(domain.InputAssociation, []domain.UserInput) (*PreparedUserInputs, error)
}

// PreparedUserInputs 持有已验证的输入关联及恰好一次提交生命周期。
type PreparedUserInputs struct {
	association domain.InputAssociation
	commit      *preparedNativeCommit
	preview     InputPreview
}

// NewPreparedUserInputs 创建显式提交/丢弃的用户输入增量。
func NewPreparedUserInputs(association domain.InputAssociation, envelope NativeCommitEnvelope, finalize, discard func(), preview InputPreview) (*PreparedUserInputs, error) {
	if err := association.Validate(); err != nil {
		return nil, err
	}
	association, err := domain.NewInputAssociation(association.TurnID(), association.Placement(), association.SampleIndex(), association.IDs())
	if err != nil {
		return nil, err
	}
	commit, err := newPreparedNativeCommit(envelope, finalize, discard)
	if err != nil {
		return nil, err
	}
	if preview.checkRequest == nil {
		return nil, fmt.Errorf("input preview is required")
	}
	return &PreparedUserInputs{association: association, commit: commit, preview: preview}, nil
}

// Association 返回不共享可变存储的输入关联。
func (inputs *PreparedUserInputs) Association() (domain.InputAssociation, error) {
	if inputs == nil || inputs.commit == nil || inputs.commit.state.Load() != 0 {
		return domain.InputAssociation{}, fmt.Errorf("prepared user inputs are not pending")
	}
	a := inputs.association
	return domain.NewInputAssociation(a.TurnID(), a.Placement(), a.SampleIndex(), a.IDs())
}

// Envelope 返回独立原生提交副本。
func (inputs *PreparedUserInputs) Envelope() (NativeCommitEnvelope, error) {
	if inputs == nil {
		return NativeCommitEnvelope{}, fmt.Errorf("prepared user inputs are invalid")
	}
	return inputs.commit.envelopeCopy()
}

// Finalize 仅在成功 Sync 后提交内存历史。
func (inputs *PreparedUserInputs) Finalize() error {
	if inputs == nil {
		return fmt.Errorf("prepared user inputs are invalid")
	}
	return inputs.commit.resolve(true)
}

// Discard 在未提交时释放 staging；不能把已提交输入变为可重试输入。
func (inputs *PreparedUserInputs) Discard() error {
	if inputs == nil {
		return fmt.Errorf("prepared user inputs are invalid")
	}
	return inputs.commit.resolve(false)
}

// Finalized 判断输入是否完成内存提交。
func (inputs *PreparedUserInputs) Finalized() bool {
	return inputs != nil && inputs.commit != nil && inputs.commit.state.Load() == 1
}

// preparedNativeCommit 组合复用不携带 sample usage 的提交生命周期。
type preparedNativeCommit struct {
	envelope NativeCommitEnvelope
	finalize func()
	discard  func()
	state    atomic.Uint32
	resolved chan struct{}
}

func newPreparedNativeCommit(envelope NativeCommitEnvelope, finalize, discard func()) (*preparedNativeCommit, error) {
	cloned, err := envelope.Clone()
	if err != nil {
		return nil, err
	}
	if finalize == nil || discard == nil {
		return nil, fmt.Errorf("native commit lifecycle callbacks are required")
	}
	return &preparedNativeCommit{envelope: cloned, finalize: finalize, discard: discard, resolved: make(chan struct{})}, nil
}

func (commit *preparedNativeCommit) envelopeCopy() (NativeCommitEnvelope, error) {
	if commit == nil || commit.finalize == nil || commit.state.Load() != 0 {
		return NativeCommitEnvelope{}, fmt.Errorf("native commit is not pending")
	}
	return commit.envelope.Clone()
}

func (commit *preparedNativeCommit) resolve(finalize bool) error {
	if commit == nil || commit.finalize == nil || commit.discard == nil {
		return fmt.Errorf("native commit is invalid")
	}
	if !commit.state.CompareAndSwap(0, 3) {
		<-commit.resolved
		return fmt.Errorf("native commit is already resolved")
	}
	if finalize {
		commit.finalize()
		commit.state.Store(1)
	} else {
		commit.discard()
		commit.state.Store(2)
	}
	close(commit.resolved)
	return nil
}

// InputPreview 保存同一候选原生历史的只读估算快照和纯内存请求校验策略。
type InputPreview struct {
	history      domain.SemanticHistoryView
	footprint    domain.NativeHistoryFootprint
	checkRequest func(TurnInput) error
}

// NewInputPreview 创建与 prepared 输入生命周期绑定的候选预检值。
func NewInputPreview(history domain.SemanticHistoryView, footprint domain.NativeHistoryFootprint, checkRequest func(TurnInput) error) (InputPreview, error) {
	if !history.Provider.Valid() || history.Turns == nil || checkRequest == nil {
		return InputPreview{}, fmt.Errorf("input preview is invalid")
	}
	for _, turn := range history.Turns {
		if err := turn.Validate(); err != nil {
			return InputPreview{}, err
		}
	}
	if err := footprint.Validate(); err != nil {
		return InputPreview{}, err
	}
	if footprint.Family() != history.Provider {
		return InputPreview{}, fmt.Errorf("preview provider mismatch")
	}
	return InputPreview{history: history.Clone(), footprint: footprint, checkRequest: checkRequest}, nil
}

// Preview 返回候选视图和原生 footprint，绝不发布到 committed history。
func (inputs *PreparedUserInputs) Preview() (domain.SemanticHistoryView, domain.NativeHistoryFootprint, error) {
	if inputs == nil || inputs.commit == nil || inputs.commit.state.Load() != 0 {
		return domain.SemanticHistoryView{}, domain.NativeHistoryFootprint{}, fmt.Errorf("prepared inputs are not pending")
	}
	return inputs.preview.history.Clone(), inputs.preview.footprint, nil
}

// CheckRequest 使用绑定的候选原生快照纯内存编译完整请求。
func (inputs *PreparedUserInputs) CheckRequest(input TurnInput) error {
	if inputs == nil || inputs.commit == nil || inputs.commit.state.Load() != 0 {
		return fmt.Errorf("prepared inputs are not pending")
	}
	return inputs.preview.checkRequest(input)
}

// UserInputEncoder 在 admission 时纯内存检查单条或前缀的原生编码大小。
type UserInputEncoder interface {
	EncodeUserInputs(domain.InputAssociation, []domain.UserInput) (NativeCommitEnvelope, error)
}
