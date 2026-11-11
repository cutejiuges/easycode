package tool

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"errors"
)

const (
	MaxPostFeedbackBytes    = 16 << 10
	PostFeedbackNoticeBytes = 512
)

type postFeedbackItem struct {
	Event   domain.HookEvent  `json:"event"`
	Source  domain.HookSource `json:"source"`
	ID      string            `json:"id"`
	Ordinal int               `json:"ordinal"`
	Content string            `json:"content,omitempty"`
	Code    domain.HookCode   `json:"code,omitempty"`
}

// RenderPostFeedback 只组合已验证且非空的冻结项，顺序由调用事实确定。
func RenderPostFeedback(items []domain.FrozenHookFeedback) (string, error) {
	out := make([]postFeedbackItem, 0, len(items))
	for _, v := range items {
		if err := v.Validate(); err != nil {
			return "", err
		}
		if v.Kind == domain.HookFeedbackEmpty {
			continue
		}
		item := postFeedbackItem{Event: v.Identity.Event, Source: v.Reference.Source, ID: v.Reference.ID, Ordinal: v.Identity.Ordinal}
		if v.Kind == domain.HookFeedbackContent {
			item.Content = v.Content
		} else {
			item.Code = v.Code
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := codec.MarshalStable(out)
	if err != nil {
		return "", err
	}
	text := "\n\n<post_tool_use_feedback>\n" + string(b) + "\n</post_tool_use_feedback>"
	if len(text) > MaxPostFeedbackBytes {
		return "", errors.New("post feedback exceeds limit")
	}
	return text, nil
}

// PostFeedbackAvailable 先为所有 model 项及固定包装保留失败提示空间。
func PostFeedbackAvailable(plan domain.PostHookBranch, reservation int) bool {
	if plan.Validate() != nil || !plan.Event.Post() || reservation < 0 {
		return false
	}
	expected, err := domain.NewPostHookBranch(plan.Event, plan.Hooks, reservation)
	return err == nil && expected.Delivery == domain.HookDeliveryAvailable
}

// FreezePostFeedback 保留前序事实和后续提示槽；正文只能整块接受或拒绝。
func FreezePostFeedback(postPlan domain.PostHookPlan, prior []domain.FrozenHookFeedback, result domain.HookRunResult, candidate domain.HookFeedbackCandidate) (domain.HookFinished, error) {
	plan, matched := postPlan.Branch(result.Identity.Event)
	reservation := postPlan.Reservation
	ordinal := len(prior)
	id, err := domain.NewPostHookRunIdentity(postPlan, result.Identity.Event, ordinal)
	if err != nil || !matched || !plan.Event.Post() || result.Identity != id {
		return domain.HookFinished{}, domain.HookConfigInvalid
	}
	for i, v := range prior {
		expected, _ := domain.NewPostHookRunIdentity(postPlan, result.Identity.Event, i)
		if v.Validate() != nil || v.Identity != expected || v.Reference != plan.Hooks[i] {
			return domain.HookFinished{}, domain.HookConfigInvalid
		}
	}
	ref := plan.Hooks[ordinal]
	if _, err := domain.NewHookExecution(result, ref.Feedback, candidate); err != nil {
		return domain.HookFinished{}, err
	}
	empty, _ := domain.NewFrozenHookFeedback(id, ref, domain.HookFeedbackEmpty, "", domain.HookOK, 0)
	if ref.Feedback == domain.HookFeedbackNone || !PostFeedbackAvailable(plan, reservation) || result.Origin == "recovery" {
		return domain.NewHookFinished(result, empty)
	}
	if candidate.Empty() && result.Status == domain.HookContinue {
		return domain.NewHookFinished(result, empty)
	}
	before, err := RenderPostFeedback(prior)
	if err != nil {
		return domain.HookFinished{}, err
	}
	remaining := 0
	for _, r := range plan.Hooks[ordinal+1:] {
		if r.Feedback == domain.HookFeedbackModel {
			remaining++
		}
	}
	kind, content, code := domain.HookFeedbackCode, "", result.Code
	if !candidate.Empty() {
		kind, content, code = domain.HookFeedbackContent, candidate.Content(), domain.HookOK
	}
	// 暂用有界正增量完成编码，最终增量必须由实际 canonical 包装计算。
	frozen, _ := domain.NewFrozenHookFeedback(id, ref, kind, content, code, 1)
	current := append(append([]domain.FrozenHookFeedback(nil), prior...), frozen)
	after, renderErr := RenderPostFeedback(current)
	if renderErr != nil || len(after)+remaining*PostFeedbackNoticeBytes > reservation {
		if kind != domain.HookFeedbackContent {
			return domain.HookFinished{}, errors.New("post feedback notice reservation violated")
		}
		result.Status, result.Code = domain.HookError, domain.HookFeedbackBudgetExceeded
		frozen, _ = domain.NewFrozenHookFeedback(id, ref, domain.HookFeedbackCode, "", result.Code, 1)
		current[len(current)-1] = frozen
		after, err = RenderPostFeedback(current)
		if err != nil || len(after)+remaining*PostFeedbackNoticeBytes > reservation {
			return domain.HookFinished{}, errors.New("post feedback notice reservation violated")
		}
	}
	frozen.BudgetDelta = len(after) - len(before)
	return domain.NewHookFinished(result, frozen)
}
