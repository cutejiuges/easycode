package domain

import "easycode/internal/codec"

// StopDisposition 表达回合完成决定，不替代单项命令的运行结果。
type StopDisposition string

const (
	StopPassed    StopDisposition = "passed"
	StopContinued StopDisposition = "continued"
	StopBlocked   StopDisposition = "blocked"
	StopError     StopDisposition = "error"
	StopCancelled StopDisposition = "cancelled"
	StopUncertain StopDisposition = "outcome_uncertain"
)

// HookOrigin 是可校验的执行或离线恢复来源。
type HookOrigin string

const (
	HookOriginExecution HookOrigin = "execution"
	HookOriginRecovery  HookOrigin = "recovery"
)

func (v HookOrigin) Valid() bool { return v == HookOriginExecution || v == HookOriginRecovery }

// StopDecisionSource 将整体计划决定与某一真实声明严格分开。
type StopDecisionSource struct {
	hook    bool
	ordinal int
	valid   bool
}

func NewStopPlanSource() StopDecisionSource { return StopDecisionSource{valid: true} }
func NewStopHookSource(ordinal int) (StopDecisionSource, error) {
	v := StopDecisionSource{true, ordinal, true}
	return v, v.Validate()
}
func (v StopDecisionSource) HookOrdinal() (int, bool) { return v.ordinal, v.valid && v.hook }
func (v StopDecisionSource) Validate() error {
	if !v.valid || v.hook && (v.ordinal < 0 || v.ordinal >= MaxHookMatches) || !v.hook && v.ordinal != 0 {
		return HookConfigInvalid
	}
	return nil
}
func (v StopDecisionSource) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	if !v.hook {
		return codec.MarshalStable(struct {
			Scope string `json:"scope"`
		}{"plan"})
	}
	return codec.MarshalStable(struct {
		Scope   string `json:"scope"`
		Ordinal int    `json:"ordinal"`
	}{"hook", v.ordinal})
}
func DecodeStopDecisionSource(b []byte) (StopDecisionSource, error) {
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return StopDecisionSource{}, HookConfigInvalid
	}
	var scope string
	if codec.UnmarshalRecord(m["scope"], &scope) != nil {
		return StopDecisionSource{}, HookConfigInvalid
	}
	if scope == "plan" {
		var w struct {
			Scope string `json:"scope"`
		}
		if codec.DecodeObject(b, &w, "scope") != nil {
			return StopDecisionSource{}, HookConfigInvalid
		}
		return NewStopPlanSource(), nil
	}
	var w struct {
		Scope   string `json:"scope"`
		Ordinal int    `json:"ordinal"`
	}
	if scope != "hook" || codec.DecodeObject(b, &w, "scope", "ordinal") != nil {
		return StopDecisionSource{}, HookConfigInvalid
	}
	return NewStopHookSource(w.Ordinal)
}
func (v *StopDecisionSource) UnmarshalJSON(b []byte) error {
	next, err := DecodeStopDecisionSource(b)
	if err != nil {
		return err
	}
	*v = next
	return nil
}

// StopDecision 仅在 continued 分支保存已接纳的最终模型文本。
type StopDecision struct {
	Target        HookTarget         `json:"target"`
	PlanDigest    string             `json:"plan_digest"`
	Processed     int                `json:"processed"`
	Disposition   StopDisposition    `json:"disposition"`
	Code          HookCode           `json:"code"`
	Origin        HookOrigin         `json:"origin"`
	Source        StopDecisionSource `json:"source"`
	Continuations uint8              `json:"continuations"`
	feedback      *StopFeedback
}
type stopDecisionWire struct {
	Target        HookTarget         `json:"target"`
	PlanDigest    string             `json:"plan_digest"`
	Processed     int                `json:"processed"`
	Disposition   StopDisposition    `json:"disposition"`
	Code          HookCode           `json:"code"`
	Origin        HookOrigin         `json:"origin"`
	Source        StopDecisionSource `json:"source"`
	Continuations uint8              `json:"continuations"`
	Feedback      *StopFeedback      `json:"feedback,omitempty"`
}

func NewStopDecision(p StopPlan, disposition StopDisposition, code HookCode, origin HookOrigin, source StopDecisionSource, finished []HookFinished, feedback StopFeedback) (StopDecision, error) {
	v := StopDecision{Target: p.target, PlanDigest: p.digest, Processed: len(finished), Disposition: disposition, Code: code, Origin: origin, Source: source, Continuations: p.continuations}
	if disposition == StopContinued {
		v.Continuations++
	}
	if !feedback.Empty() {
		v.feedback = &feedback
	}
	return v, v.ValidateAgainst(p, finished)
}
func (v StopDecision) Feedback() (StopFeedback, bool) {
	if v.feedback == nil {
		return StopFeedback{}, false
	}
	return *v.feedback, true
}
func (v StopDecision) Clone() StopDecision {
	if v.feedback != nil {
		f := *v.feedback
		v.feedback = &f
	}
	return v
}
func (v StopDecision) wire() stopDecisionWire {
	v = v.Clone()
	return stopDecisionWire{v.Target, v.PlanDigest, v.Processed, v.Disposition, v.Code, v.Origin, v.Source, v.Continuations, v.feedback}
}
func (v StopDecision) Validate() error {
	if !v.Target.ValidFor(HookStop) || !ValidHookDigest(v.PlanDigest) || v.Processed < 0 || v.Processed > MaxHookMatches || !v.Origin.Valid() || v.Source.Validate() != nil || v.Continuations > MaxStopContinuations {
		return HookConfigInvalid
	}
	if (v.feedback != nil) != (v.Disposition == StopContinued) {
		return HookConfigInvalid
	}
	switch v.Disposition {
	case StopPassed:
		if v.Code != HookOK || v.Origin != HookOriginExecution || v.Source.hook || v.Processed == 0 {
			return HookConfigInvalid
		}
	case StopContinued:
		f, ok := v.Feedback()
		sample, _ := v.Target.AssistantSample()
		if !ok || v.Code != HookDenied || v.Origin != HookOriginExecution || !v.Source.hook || v.Source.ordinal != v.Processed-1 || v.Continuations < 1 || sample.SampleIndex+1 >= 16 || f.Validate() != nil || f.Identity.Target != v.Target || f.Identity.PlanDigest != v.PlanDigest || f.Identity.Ordinal != v.Source.ordinal {
			return HookConfigInvalid
		}
	case StopBlocked:
		switch v.Code {
		case HookDenied, HookTrustDenied, HookTrustRequired, HookStopLimitExceeded, HookSampleLimitExceeded:
		default:
			return HookConfigInvalid
		}
		if v.Origin != HookOriginExecution {
			return HookConfigInvalid
		}
	case StopError:
		switch v.Code {
		case HookConfigInvalid, HookMatchLimit, HookInputLimit, HookStartFailed, HookExitFailed, HookOutputInvalid, HookOutputLimit, HookIOFailed, HookTimedOut, HookFeedbackBudgetExceeded, HookFeedbackEncodingFailed, HookExecutionBudgetExhausted, HookUnsupported, HookSandboxUnavailable:
		default:
			return HookConfigInvalid
		}
		if v.Origin == HookOriginRecovery && v.Code != HookConfigInvalid && v.Code != HookMatchLimit {
			return HookConfigInvalid
		}
	case StopCancelled:
		if v.Origin == HookOriginRecovery {
			if v.Code != HookStopInterrupted || v.Source.hook {
				return HookConfigInvalid
			}
		} else if v.Code != HookCancelled {
			return HookConfigInvalid
		}
	case StopUncertain:
		if v.Code != HookOutcomeUncertain || !v.Source.hook || v.Processed == 0 {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	return nil
}

func (v StopDecision) ValidateAgainst(p StopPlan, finished []HookFinished) error {
	if v.Validate() != nil || p.Validate() != nil || v.Target != p.target || v.PlanDigest != p.digest || v.Processed != len(finished) || v.Processed > len(p.checks) {
		return HookConfigInvalid
	}
	want := p.continuations
	if v.Disposition == StopContinued {
		want++
	}
	if v.Continuations != want {
		return HookConfigInvalid
	}
	for i, f := range finished {
		id, err := NewStopHookRunIdentity(p, i)
		if err != nil || f.Validate() != nil || f.Result.Identity != id || f.Feedback.Reference != p.checks[i] || f.Feedback.Kind != HookFeedbackEmpty || i < len(finished)-1 && f.Result.Status != HookContinue {
			return HookConfigInvalid
		}
	}
	if p.kind == StopPlanUnavailable {
		if v.Source.hook || len(finished) != 0 || v.Disposition != StopError && v.Disposition != StopCancelled || v.Disposition == StopError && v.Code != p.code {
			return HookConfigInvalid
		}
		return nil
	}
	if v.Source.hook {
		if v.Source.ordinal >= len(p.checks) {
			return HookConfigInvalid
		}
		if v.Code == HookTrustDenied {
			if v.Processed != 0 || v.Disposition != StopBlocked {
				return HookConfigInvalid
			}
			return nil
		}
		if v.Processed == 0 || v.Source.ordinal != v.Processed-1 {
			return HookConfigInvalid
		}
		last := finished[v.Processed-1].Result
		switch v.Disposition {
		case StopContinued:
			f, _ := v.Feedback()
			if p.continuations >= MaxStopContinuations || last.Status != HookBlock || last.Code != HookDenied || !last.ExitKnown || last.ExitCode != 0 || !last.CleanupConfirmed || last.Cancelled || last.TimedOut || last.Origin != "execution" || f.Identity != last.Identity || f.Reference != p.checks[v.Source.ordinal] {
				return HookConfigInvalid
			}
		case StopBlocked:
			if last.Status != HookBlock || last.Code != HookDenied || v.Code != HookDenied && v.Code != HookStopLimitExceeded && v.Code != HookSampleLimitExceeded {
				return HookConfigInvalid
			}
			if v.Code != HookDenied {
				if last.ExitCode != 0 || p.checks[v.Source.ordinal].Feedback != HookFeedbackModel {
					return HookConfigInvalid
				}
				if v.Code == HookStopLimitExceeded && p.continuations != MaxStopContinuations || v.Code == HookSampleLimitExceeded && (p.continuations == MaxStopContinuations || p.Sample().SampleIndex+1 < 16) {
					return HookConfigInvalid
				}
			}
		case StopError:
			if last.Status == HookError {
				if v.Code != last.Code {
					return HookConfigInvalid
				}
			} else if last.Status != HookBlock || v.Code != HookFeedbackBudgetExceeded && v.Code != HookFeedbackEncodingFailed {
				return HookConfigInvalid
			}
		case StopUncertain:
			if last.Status != HookUncertain || HookOrigin(last.Origin) != v.Origin {
				return HookConfigInvalid
			}
		case StopCancelled:
			if last.Status != HookBlock && last.Status != HookContinue && last.Status != HookCancel {
				return HookConfigInvalid
			}
		default:
			return HookConfigInvalid
		}
	} else {
		for _, f := range finished {
			if f.Result.Status != HookContinue {
				return HookConfigInvalid
			}
		}
		switch v.Disposition {
		case StopPassed:
			if len(finished) != len(p.checks) {
				return HookConfigInvalid
			}
		case StopBlocked:
			if v.Code != HookTrustRequired || len(finished) != 0 {
				return HookConfigInvalid
			}
		case StopError:
			switch v.Code {
			case HookExecutionBudgetExhausted:
			case HookInputLimit, HookUnsupported, HookSandboxUnavailable:
				if len(finished) != 0 {
					return HookConfigInvalid
				}
			default:
				return HookConfigInvalid
			}
		case StopCancelled:
		default:
			return HookConfigInvalid
		}
	}
	return nil
}
func (v StopDecision) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	return codec.MarshalStable(v.wire())
}
func DecodeStopDecision(b []byte) (StopDecision, error) {
	var w stopDecisionWire
	if len(b) > MaxHookConfigBytes || codec.DecodeObject(b, &w, "target", "plan_digest", "processed", "disposition", "code", "origin", "source", "continuations") != nil {
		return StopDecision{}, HookConfigInvalid
	}
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return StopDecision{}, HookConfigInvalid
	}
	_, has := m["feedback"]
	if has != (w.Disposition == StopContinued) || has && w.Feedback == nil {
		return StopDecision{}, HookConfigInvalid
	}
	v := StopDecision{w.Target, w.PlanDigest, w.Processed, w.Disposition, w.Code, w.Origin, w.Source, w.Continuations, w.Feedback}
	return v, v.Validate()
}
func (v *StopDecision) UnmarshalJSON(b []byte) error {
	next, err := DecodeStopDecision(b)
	if err != nil {
		return err
	}
	*v = next
	return nil
}
