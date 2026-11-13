package domain

// StopContinuationKind 区分本地反馈义务与附带真实用户纠正的交付。
type StopContinuationKind string

const (
	StopFeedbackOnly StopContinuationKind = "feedback_only"
	StopWithSteering StopContinuationKind = "with_steering"
)

// StopContinuationInput 冻结本次候选，不把 Hook 正文伪装成用户输入。
type StopContinuationInput struct {
	kind        StopContinuationKind
	feedback    StopFeedback
	association InputAssociation
	inputs      []UserInput
}

func NewStopFeedbackOnly(feedback StopFeedback) (StopContinuationInput, error) {
	v := StopContinuationInput{kind: StopFeedbackOnly, feedback: feedback}
	return v, v.Validate()
}
func NewStopWithSteering(feedback StopFeedback, association InputAssociation, inputs []UserInput) (StopContinuationInput, error) {
	v := StopContinuationInput{kind: StopWithSteering, feedback: feedback, association: association, inputs: append([]UserInput(nil), inputs...)}.Clone()
	return v, v.Validate()
}
func (v StopContinuationInput) Kind() StopContinuationKind { return v.kind }
func (v StopContinuationInput) Feedback() StopFeedback     { return v.feedback }
func (v StopContinuationInput) Inputs() []UserInput        { return append([]UserInput(nil), v.inputs...) }
func (v StopContinuationInput) Association() (InputAssociation, bool) {
	if v.kind != StopWithSteering {
		return InputAssociation{}, false
	}
	a := v.association
	copy, err := NewInputAssociation(a.TurnID(), a.Placement(), a.SampleIndex(), a.IDs())
	return copy, err == nil
}
func (v StopContinuationInput) Clone() StopContinuationInput {
	v.inputs = v.Inputs()
	if a, ok := v.Association(); ok {
		v.association = a
	}
	return v
}
func (v StopContinuationInput) Validate() error {
	if v.feedback.Validate() != nil {
		return HookConfigInvalid
	}
	if v.kind == StopFeedbackOnly {
		if len(v.inputs) != 0 || v.association.turnID != "" || v.association.placement != "" || v.association.sampleIndex != 0 || len(v.association.ids) != 0 {
			return HookConfigInvalid
		}
		return nil
	}
	if v.kind != StopWithSteering || v.association.Validate() != nil {
		return HookConfigInvalid
	}
	sample, _ := v.feedback.Identity.Target.AssistantSample()
	if sample.SampleIndex+1 >= 16 || v.association.TurnID() != sample.TurnID || v.association.SampleIndex() != sample.SampleIndex+1 || v.association.Placement() != InputSteering || len(v.inputs) != len(v.association.ids) {
		return HookConfigInvalid
	}
	for i, input := range v.inputs {
		if input.Validate() != nil || input.ID() != v.association.ids[i] {
			return HookConfigInvalid
		}
	}
	return nil
}
