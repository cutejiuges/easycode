package tool

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"strings"
	"unicode/utf8"
)

// ProjectPreInput 由已提交前缀产生候选或最终选择摘要，不依赖宿主或存储。
func ProjectPreInput(original Invocation, plan domain.HookPlan, finished []domain.HookFinished, decision *domain.HookGateDecision, sequence uint64) (protocol.PreInputProjection, error) {
	input, err := CheckingHookInput(original, plan, finished)
	if err != nil {
		return protocol.PreInputProjection{}, err
	}
	kind := protocol.PreInputCandidate
	if decision != nil {
		if decision.Status == domain.HookContinue {
			kind = protocol.PreInputSelected
		} else {
			kind = protocol.PreInputRejected
		}
	}
	digest, err := input.Digest()
	if err != nil {
		return protocol.PreInputProjection{}, err
	}
	current := original
	before, err := HookInputForInvocation(original)
	if err != nil {
		return protocol.PreInputProjection{}, err
	}
	if !before.Equal(input) {
		current, err = RewriteInvocation(original, input)
	}
	if err != nil {
		return protocol.PreInputProjection{}, err
	}
	summary := strings.NewReplacer("\n", " ", "\r", " ").Replace(ProjectActivity(current).Summary)
	if utf8.RuneCountInString(summary) > 160 {
		summary = string([]rune(summary)[:159]) + "…"
	}
	return protocol.NewPreInputProjection(kind, sequence, digest, summary)
}

// ProjectPreActivity 共用 live 与恢复后的活动映射，统计只能来自真实 finished。
func ProjectPreActivity(original Invocation, plan domain.HookPlan, finished []domain.HookFinished, started *domain.HookRunIdentity, decision *domain.HookGateDecision, sequence uint64) (protocol.HookActivityPayload, error) {
	projection, err := ProjectPreInput(original, plan, finished, decision, sequence)
	if err != nil {
		return protocol.HookActivityPayload{}, err
	}
	ordinal := min(len(finished), len(plan.Hooks)-1)
	activity := protocol.HookActivityPayload{Target: plan.Target(), PlanDigest: plan.Digest, Reference: plan.Hooks[ordinal], State: protocol.HookActivityChecking, Code: domain.HookOK, PreInput: projection}
	if len(finished) > 0 {
		last := finished[len(finished)-1]
		activity.Reference = last.Feedback.Reference
		activity.State = protocol.HookActivityState(last.Result.Status)
		activity.Code = last.Result.Code
		activity.StdoutBytes = last.Result.StdoutBytes
		activity.StderrBytes = last.Result.StderrBytes
	}
	if started != nil {
		activity.Reference = plan.Hooks[started.Ordinal]
		activity.State = protocol.HookActivityChecking
		activity.Code = domain.HookOK
		activity.StdoutBytes, activity.StderrBytes = 0, 0
	}
	if decision != nil {
		activity.State = protocol.HookActivityState(decision.Status)
		activity.Code = decision.Code
		if blocking := decision.BlockingOrdinal; blocking != nil && *blocking >= len(finished) {
			activity.Reference = plan.Hooks[*blocking]
			activity.StdoutBytes, activity.StderrBytes = 0, 0
		}
	}
	return activity, activity.Validate()
}
