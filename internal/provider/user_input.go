package provider

import (
	"fmt"

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
	if inputs == nil || inputs.commit == nil || !inputs.commit.resolution.pending() {
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
	return inputs != nil && inputs.commit != nil && inputs.commit.resolution.finalized()
}

// preparedNativeCommit 组合单信封存储与恰好一次的提交生命周期。
type preparedNativeCommit struct {
	envelope   NativeCommitEnvelope
	resolution *preparedResolution
}

func newPreparedNativeCommit(envelope NativeCommitEnvelope, finalize, discard func()) (*preparedNativeCommit, error) {
	cloned, err := envelope.Clone()
	if err != nil {
		return nil, err
	}
	r, err := newPreparedResolution(finalize, discard)
	if err != nil {
		return nil, err
	}
	return &preparedNativeCommit{cloned, r}, nil
}
func (commit *preparedNativeCommit) envelopeCopy() (NativeCommitEnvelope, error) {
	if commit == nil || !commit.resolution.pending() {
		return NativeCommitEnvelope{}, fmt.Errorf("native commit is not pending")
	}
	return commit.envelope.Clone()
}
func (commit *preparedNativeCommit) resolve(finalize bool) error {
	if commit == nil {
		return fmt.Errorf("native commit is invalid")
	}
	return commit.resolution.resolve(finalize)
}

// InputPreview 保存同一候选原生历史的只读估算快照和纯内存请求校验策略。
type InputPreview struct {
	history      domain.ActiveContextView
	footprint    domain.NativeHistoryFootprint
	checkRequest func(TurnInput) error
}

// NewInputPreview 创建与 prepared 输入生命周期绑定的候选预检值。
func NewInputPreview(history domain.ActiveContextView, footprint domain.NativeHistoryFootprint, checkRequest func(TurnInput) error) (InputPreview, error) {
	if err := history.Validate(); err != nil {
		return InputPreview{}, err
	}
	if checkRequest == nil {
		return InputPreview{}, fmt.Errorf("input preview is invalid")
	}
	if err := footprint.Validate(); err != nil {
		return InputPreview{}, err
	}
	if footprint.Family() != history.History().Provider || footprint.Revision() != history.Revision() {
		return InputPreview{}, fmt.Errorf("preview provider mismatch")
	}
	return InputPreview{history: history.Clone(), footprint: footprint, checkRequest: checkRequest}, nil
}

// Preview 返回候选视图和原生 footprint，绝不发布到 committed history。
func (inputs *PreparedUserInputs) Preview() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	if inputs == nil || inputs.commit == nil || !inputs.commit.resolution.pending() {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, fmt.Errorf("prepared inputs are not pending")
	}
	return inputs.preview.history.Clone(), inputs.preview.footprint, nil
}

// CheckRequest 使用绑定的候选原生快照纯内存编译完整请求。
func (inputs *PreparedUserInputs) CheckRequest(input TurnInput) error {
	if inputs == nil || inputs.commit == nil || !inputs.commit.resolution.pending() {
		return fmt.Errorf("prepared inputs are not pending")
	}
	return inputs.preview.checkRequest(input)
}

// UserInputEncoder 在 admission 时纯内存检查单条或前缀的原生编码大小。
type UserInputEncoder interface {
	EncodeUserInputs(domain.InputAssociation, []domain.UserInput) (NativeCommitEnvelope, error)
}
