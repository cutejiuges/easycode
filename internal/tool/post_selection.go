package tool

import (
	"errors"

	"easycode/internal/domain"
)

// ExecutionAdmission 证明同一调用的执行尝试已成功持久化，不表示进程已经启动。
type ExecutionAdmission struct {
	invocation InvocationID
	sequence   uint64
}

func NewExecutionAdmission(invocation InvocationID, sequence uint64) (ExecutionAdmission, error) {
	v := ExecutionAdmission{invocation, sequence}
	if !invocation.Valid() || sequence == 0 {
		return ExecutionAdmission{}, errors.New("invalid execution admission")
	}
	return v, nil
}

func (v ExecutionAdmission) InvocationID() InvocationID { return v.invocation }
func (v ExecutionAdmission) Sequence() uint64           { return v.sequence }

// SelectPostHookRoute 仅消费原结果和同调用准入事实，取消及预算不参与选择。
func SelectPostHookRoute(result InvocationResult, admission ExecutionAdmission) (domain.PostHookRoute, error) {
	if result.Validate() != nil {
		return "", errors.New("invalid post hook result")
	}
	if admission.sequence == 0 {
		if admission.invocation != "" {
			return "", errors.New("incomplete execution admission")
		}
		return domain.PostHookRouteNone, nil
	}
	if admission.invocation != result.InvocationID() || !admission.invocation.Valid() {
		return "", errors.New("post hook admission mismatch")
	}
	switch result.Status() {
	case ResultSuccess:
		return domain.PostHookRouteSuccess, nil
	case ResultError, ResultConflict:
		return domain.PostHookRouteFailure, nil
	default:
		return domain.PostHookRouteNone, nil
	}
}

// PostHookSelection 绑定不可变计划、准入和原结果的选择证据。
type PostHookSelection struct {
	plan      domain.PostHookPlan
	admission ExecutionAdmission
	route     domain.PostHookRoute
}

func NewPostHookSelection(plan domain.PostHookPlan, admission ExecutionAdmission, result InvocationResult) (PostHookSelection, error) {
	if plan.Validate() != nil || plan.InvocationID != string(result.InvocationID()) || string(plan.Capability) != string(result.Capability()) {
		return PostHookSelection{}, errors.New("post hook selection ownership mismatch")
	}
	route, err := SelectPostHookRoute(result, admission)
	if err != nil {
		return PostHookSelection{}, err
	}
	return PostHookSelection{plan.Clone(), admission, route}, nil
}

func (v PostHookSelection) Route() domain.PostHookRoute { return v.route }

func (v PostHookSelection) validate(result InvocationResult, feedback []domain.FrozenHookFeedback) error {
	expected, err := NewPostHookSelection(v.plan, v.admission, result)
	if err != nil || expected.route != v.route {
		return errors.New("invalid post hook selection")
	}
	if result.Budget().Allowance() != v.plan.Allowance-v.plan.Reservation {
		return errors.New("post hook output base allowance mismatch")
	}
	event, selected := v.route.Event()
	branch, matched := v.plan.Branch(event)
	if len(feedback) > 0 && (!selected || !matched) {
		return errors.New("unselected post hook feedback")
	}
	for _, f := range feedback {
		i := f.Identity.Ordinal
		if f.Validate() != nil || f.Identity.Event != event || f.Identity.PlanDigest != v.plan.Digest || f.Identity.InvocationID != v.plan.InvocationID || i < 0 || i >= len(branch.Hooks) || f.Reference != branch.Hooks[i] {
			return errors.New("post hook feedback ownership mismatch")
		}
		if f.Kind != domain.HookFeedbackEmpty && branch.Delivery != domain.HookDeliveryAvailable {
			return errors.New("post hook feedback delivery unavailable")
		}
	}
	text, err := RenderPostFeedback(feedback)
	if err != nil || len(text) > v.plan.Reservation {
		return errors.New("post hook output feedback budget mismatch")
	}
	return nil
}
