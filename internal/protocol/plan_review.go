package protocol

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const maxPlanReviewPayloadBytes = 6*domain.MaxPlanProposalBytes + 256

// PlanReviewRef 指向当前会话中一份已提交方案的原生采样。
type PlanReviewRef struct {
	SessionID domain.SessionID
	ThreadID  domain.ThreadID
	Sample    domain.AssistantSampleReference
}

// Validate 校验完整会话与采样身份。
func (ref PlanReviewRef) Validate() error {
	if !ref.SessionID.Valid() || !ref.ThreadID.Valid() || ref.Sample.Validate() != nil {
		return fmt.Errorf("plan review identity is invalid")
	}
	return nil
}

// NewApprovePlanCommand 创建不携带正文或工具授权的交接请求。
func NewApprovePlanCommand(requestID RequestID, ref PlanReviewRef) (Command, error) {
	command := Command{kind: CommandApprovePlan, requestID: requestID, planReview: ref}
	return command, command.Validate()
}

// PlanReviewRef 返回确认命令的方案身份。
func (command Command) PlanReviewRef() PlanReviewRef { return command.planReview }

// PlanReviewOffer 关联原生提交和可供终端审阅的完整 Markdown。
type PlanReviewOffer struct {
	Ref      PlanReviewRef
	Markdown string
}

type planReviewPayload struct {
	SampleIndex uint32 `json:"sample_index"`
	Markdown    string `json:"markdown"`
}

// NewPlanReviewOfferItem 创建有序的宿主通知，关联已结束的规划回合。
func NewPlanReviewOfferItem(requestID RequestID, ref PlanReviewRef, markdown string) (ControlItem, error) {
	if err := (PlanReviewOffer{Ref: ref, Markdown: markdown}).validate(); err != nil {
		return ControlItem{}, err
	}
	encoded, err := codec.MarshalStable(planReviewPayload{SampleIndex: ref.Sample.SampleIndex, Markdown: markdown})
	if err != nil {
		return ControlItem{}, err
	}
	event, err := newScopedPayloadEvent(EventPlanReviewOffered, ref.SessionID, ref.ThreadID, "", ref.Sample.TurnID, encoded)
	if err != nil {
		return ControlItem{}, err
	}
	return NewCorrelatedContextItem(requestID, event)
}

// DecodePlanReviewOffer 严格解码进程内审阅通知的身份和正文。
func DecodePlanReviewOffer(event Event) (PlanReviewOffer, error) {
	if event.Kind != EventPlanReviewOffered || event.CompactionID != "" || len(event.Payload) > maxPlanReviewPayloadBytes {
		return PlanReviewOffer{}, fmt.Errorf("plan review offer is invalid")
	}
	var p planReviewPayload
	if err := codec.UnmarshalRecord(event.Payload, &p); err != nil {
		return PlanReviewOffer{}, err
	}
	ref := PlanReviewRef{
		SessionID: event.SessionID, ThreadID: event.ThreadID,
		Sample: domain.AssistantSampleReference{TurnID: event.TurnID, SampleIndex: p.SampleIndex},
	}
	offer := PlanReviewOffer{Ref: ref, Markdown: p.Markdown}
	return offer, offer.validate()
}

func (offer PlanReviewOffer) validate() error {
	if offer.Ref.Validate() != nil || len(offer.Markdown) > domain.MaxPlanProposalBytes ||
		!utf8.ValidString(offer.Markdown) || strings.TrimSpace(offer.Markdown) == "" {
		return fmt.Errorf("plan review offer payload is invalid")
	}
	return nil
}

// PlanReviewOffer 返回不属于实时回合事件序列的审阅通知。
func (item ControlItem) PlanReviewOffer() (PlanReviewOffer, bool) {
	if item.kind != ControlItemContextEvent || item.event.Kind != EventPlanReviewOffered {
		return PlanReviewOffer{}, false
	}
	offer, err := DecodePlanReviewOffer(item.event)
	return offer, err == nil
}
