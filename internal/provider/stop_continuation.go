package provider

import (
	"fmt"

	"easycode/internal/domain"
)

// StopContinuationPreparer 一次准备一/两个原生增量及完整候选请求，不建立网络。
type StopContinuationPreparer interface {
	PrepareStopContinuation(domain.StopContinuationInput) (*PreparedStopContinuation, error)
}

// StopFeedbackEncoder 在冻结决定之前验证单独反馈的本地可表示性。
type StopFeedbackEncoder interface {
	EncodeStopFeedback(domain.StopFeedback) (NativeCommitEnvelope, error)
}

// PreparedStopContinuation 共享一个 pending owner 和 resolution，绝不串联两个 preparer。
type PreparedStopContinuation struct {
	input      domain.StopContinuationInput
	feedback   NativeCommitEnvelope
	steering   NativeCommitEnvelope
	resolution *preparedResolution
	preview    InputPreview
}

func NewPreparedStopContinuation(input domain.StopContinuationInput, feedback NativeCommitEnvelope, finalize, discard func(), preview InputPreview) (*PreparedStopContinuation, error) {
	if input.Kind() != domain.StopFeedbackOnly {
		return nil, fmt.Errorf("feedback-only continuation is required")
	}
	return newPreparedStopContinuation(input, feedback, NativeCommitEnvelope{}, finalize, discard, preview)
}
func NewPreparedSteeredStopContinuation(input domain.StopContinuationInput, feedback, steering NativeCommitEnvelope, finalize, discard func(), preview InputPreview) (*PreparedStopContinuation, error) {
	if input.Kind() != domain.StopWithSteering {
		return nil, fmt.Errorf("steered continuation is required")
	}
	return newPreparedStopContinuation(input, feedback, steering, finalize, discard, preview)
}
func newPreparedStopContinuation(input domain.StopContinuationInput, feedback, steering NativeCommitEnvelope, finalize, discard func(), preview InputPreview) (*PreparedStopContinuation, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	f, err := feedback.Clone()
	if err != nil {
		return nil, err
	}
	if input.Kind() == domain.StopWithSteering {
		steering, err = steering.Clone()
		if err != nil || steering.Family() != f.Family() || steering.Wire() != f.Wire() {
			return nil, fmt.Errorf("continuation envelopes do not match")
		}
	}
	if preview.checkRequest == nil || preview.footprint.Family() != f.Family() {
		return nil, fmt.Errorf("continuation preview is invalid")
	}
	r, err := newPreparedResolution(finalize, discard)
	if err != nil {
		return nil, err
	}
	return &PreparedStopContinuation{input.Clone(), f, steering, r, preview}, nil
}
func (v *PreparedStopContinuation) pending() bool { return v != nil && v.resolution.pending() }
func (v *PreparedStopContinuation) Input() (domain.StopContinuationInput, error) {
	if !v.pending() {
		return domain.StopContinuationInput{}, fmt.Errorf("continuation is not pending")
	}
	return v.input.Clone(), nil
}
func (v *PreparedStopContinuation) FeedbackEnvelope() (NativeCommitEnvelope, error) {
	if !v.pending() {
		return NativeCommitEnvelope{}, fmt.Errorf("continuation is not pending")
	}
	return v.feedback.Clone()
}
func (v *PreparedStopContinuation) SteeringEnvelope() (NativeCommitEnvelope, bool, error) {
	if !v.pending() {
		return NativeCommitEnvelope{}, false, fmt.Errorf("continuation is not pending")
	}
	if v.input.Kind() != domain.StopWithSteering {
		return NativeCommitEnvelope{}, false, nil
	}
	envelope, err := v.steering.Clone()
	return envelope, true, err
}
func (v *PreparedStopContinuation) Preview() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	if !v.pending() {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, fmt.Errorf("continuation is not pending")
	}
	return v.preview.history.Clone(), v.preview.footprint, nil
}
func (v *PreparedStopContinuation) CheckRequest(input TurnInput) error {
	if !v.pending() {
		return fmt.Errorf("continuation is not pending")
	}
	return v.preview.checkRequest(input)
}
func (v *PreparedStopContinuation) Finalize() error {
	if v == nil {
		return fmt.Errorf("continuation is invalid")
	}
	return v.resolution.resolve(true)
}
func (v *PreparedStopContinuation) Discard() error {
	if v == nil {
		return fmt.Errorf("continuation is invalid")
	}
	return v.resolution.resolve(false)
}
func (v *PreparedStopContinuation) Finalized() bool { return v != nil && v.resolution.finalized() }

// HookFeedbackMetadata 不重复模型正文，只保留核对原生消息所需的安全来源。
type HookFeedbackMetadata struct {
	Source domain.HookMessageSource `json:"source"`
	Bytes  int                      `json:"bytes"`
	Digest string                   `json:"digest"`
}

func NewHookFeedbackMetadata(feedback domain.StopFeedback) (HookFeedbackMetadata, error) {
	source, err := domain.NewHookMessageSource(feedback)
	if err != nil {
		return HookFeedbackMetadata{}, err
	}
	return HookFeedbackMetadata{source, feedback.Bytes, feedback.Digest}, nil
}
func (v HookFeedbackMetadata) Feedback(text string) (domain.StopFeedback, error) {
	if v.Source.Validate() != nil {
		return domain.StopFeedback{}, fmt.Errorf("invalid hook feedback source")
	}
	f := domain.StopFeedback{Identity: v.Source.Identity, Reference: v.Source.Reference, Text: text, Bytes: v.Bytes, Digest: v.Digest}
	return f, f.Validate()
}
