package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// UserInputCommittedPayload 是已成功保存的单条用户输入展示事实。
type UserInputCommittedPayload struct {
	Files   domain.FileSummary      `json:"files"`
	Skill   *domain.SkillAttachment `json:"skill,omitempty"`
	InputID domain.InputID          `json:"input_id"`
	Text    string                  `json:"text"`
}

// NewUserInputCommitted 创建输入已保存事件，不替代原生历史。
func NewUserInputCommitted(input domain.UserInput) (Event, error) {
	if err := input.Validate(); err != nil {
		return Event{}, err
	}
	p := UserInputCommittedPayload{InputID: input.ID(), Text: input.Text()}
	summary, err := domain.NewFileSummary(input.FileAttachments())
	if err != nil {
		return Event{}, err
	}
	p.Files = summary
	if a, ok := input.SkillAttachment(); ok {
		p.Skill = &a
	}
	return newEventWithPayload(EventUserInputCommitted, p)
}

// DecodeUserInputCommitted 严格解码并验证用户输入事实。
func DecodeUserInputCommitted(event Event) (UserInputCommittedPayload, error) {
	if event.Kind != EventUserInputCommitted {
		return UserInputCommittedPayload{}, fmt.Errorf("event kind is not user_input_committed")
	}
	var p UserInputCommittedPayload
	if err := codec.UnmarshalRecord(event.Payload, &p); err != nil {
		return p, err
	}
	v, err := domain.NewUserInput(p.InputID, p.Text)
	if err == nil && p.Skill != nil {
		_, err = v.WithSkillAttachment(*p.Skill)
	}
	if err == nil && p.Files.Total() > 0 {
		refs, parseErr := domain.ParseFileReferences(p.Text)
		if parseErr != nil || len(refs) != p.Files.Total() {
			return p, fmt.Errorf("file summary does not match input")
		}
		for i, item := range p.Files.Items() {
			if item.Reference() != refs[i] {
				return p, fmt.Errorf("file summary order does not match input")
			}
		}
	}
	return p, err
}

// InputState 是自动输入的可观察投递状态，不承诺远端处理。
type InputState string

const (
	InputClaimed   InputState = "claimed"
	InputCommitted InputState = "committed"
	InputReturned  InputState = "returned"
	InputUncertain InputState = "uncertain"
)

// InputReason 是不回显正文的固定输入处理原因。
type InputReason string

const (
	InputReasonRetracted    InputReason = "retracted"
	InputReasonStopped      InputReason = "stopped"
	InputReasonCancelled    InputReason = "cancelled"
	InputReasonShutdown     InputReason = "session_shutdown"
	InputReasonFailed       InputReason = "session_failed"
	InputReasonPreflight    InputReason = "input_preflight_failed"
	InputReasonUncertain    InputReason = "persistence_uncertain"
	InputReasonHookRejected InputReason = "hook_rejected"
)

// InputStatus 保持命令、输入与实际 turn 的独立身份。
type InputStatus struct {
	requestID     RequestID
	entryID       domain.InputID
	state         InputState
	turnID        domain.TurnID
	reason        InputReason
	hookCode      domain.HookCode
	hookSource    domain.HookSource
	declarationID string
}

// NewInputStatus 创建不含正文的输入状态投影。
func NewInputStatus(requestID RequestID, entryID domain.InputID, state InputState, turnID domain.TurnID, reason InputReason) (InputStatus, error) {
	status := InputStatus{requestID: requestID, entryID: entryID, state: state, turnID: turnID, reason: reason}
	return status, status.Validate()
}

// NewHookRejectedInputStatus 创建只含固定诊断和可选声明身份的本地返还状态。
func NewHookRejectedInputStatus(requestID RequestID, entryID domain.InputID, turnID domain.TurnID, code domain.HookCode, source domain.HookSource, declarationID string) (InputStatus, error) {
	status := InputStatus{
		requestID: requestID, entryID: entryID, state: InputReturned, turnID: turnID, reason: InputReasonHookRejected,
		hookCode: code, hookSource: source, declarationID: declarationID,
	}
	return status, status.Validate()
}

func (status InputStatus) RequestID() RequestID    { return status.requestID }
func (status InputStatus) EntryID() domain.InputID { return status.entryID }
func (status InputStatus) State() InputState       { return status.state }
func (status InputStatus) TurnID() domain.TurnID   { return status.turnID }
func (status InputStatus) Reason() InputReason     { return status.reason }

// HookRejection 返回不包含命令、输入正文或脚本输出的检查诊断。
func (status InputStatus) HookRejection() (domain.HookCode, domain.HookSource, string, bool) {
	return status.hookCode, status.hookSource, status.declarationID, status.reason == InputReasonHookRejected
}

// Validate 验证状态与原因及 turn 归属的封闭组合。
func (status InputStatus) Validate() error {
	if !status.requestID.Valid() || !status.entryID.Valid() || (status.turnID != "" && !status.turnID.Valid()) {
		return fmt.Errorf("input status identity is invalid")
	}
	if status.reason == InputReasonHookRejected {
		if status.state != InputReturned || !status.turnID.Valid() || !domain.ValidUserPromptSubmitRejection(status.hookCode) ||
			((status.hookSource != "" || status.declarationID != "") && (!status.hookSource.Valid() || domain.ValidateSkillName(status.declarationID) != nil)) {
			return fmt.Errorf("hook input rejection is invalid")
		}
		return nil
	}
	if status.hookCode != "" || status.hookSource != "" || status.declarationID != "" {
		return fmt.Errorf("unexpected hook input rejection")
	}
	switch status.state {
	case InputClaimed, InputCommitted:
		if !status.turnID.Valid() || status.reason != "" {
			return fmt.Errorf("input status payload is invalid")
		}
	case InputUncertain:
		if !status.turnID.Valid() || status.reason != InputReasonUncertain {
			return fmt.Errorf("uncertain input status is invalid")
		}
	case InputReturned:
		switch status.reason {
		case InputReasonRetracted, InputReasonStopped, InputReasonCancelled, InputReasonShutdown, InputReasonFailed, InputReasonPreflight:
		default:
			return fmt.Errorf("returned input reason is invalid")
		}
	default:
		return fmt.Errorf("input status state is invalid")
	}
	return nil
}

// NewInputStatusItem 创建单一控制消费者接收的输入状态。
func NewInputStatusItem(status InputStatus) (ControlItem, error) {
	item := ControlItem{kind: ControlItemInputStatus, requestID: status.requestID, inputStatus: status}
	return item, item.Validate()
}

// InputStatus 返回封闭控制项中的输入状态。
func (item ControlItem) InputStatus() (InputStatus, bool) {
	return item.inputStatus, item.kind == ControlItemInputStatus
}

// NewAcceptedInputResult 创建自动输入 admission 的领域身份反馈。
func NewAcceptedInputResult(command Command, disposition CommandDisposition, id domain.InputID) (CommandResult, error) {
	if (command.Kind() != CommandSubmitConversationalInput && command.Kind() != CommandApprovePlan) || !id.Valid() {
		return CommandResult{}, fmt.Errorf("automatic input result is invalid")
	}
	result := CommandResult{requestID: command.RequestID(), commandKind: command.Kind(), status: CommandStatusAccepted, disposition: disposition, entryID: id}
	if err := command.Validate(); err != nil {
		return CommandResult{}, err
	}
	return result, result.Validate()
}

// EntryID 返回自动输入的独立身份；严格提交和拒绝返回零值。
func (result CommandResult) EntryID() domain.InputID { return result.entryID }
