package protocol

import (
	"easycode/internal/domain"
	"fmt"
)

// CommandStatus 描述命令是否被 owner 接受。
type CommandStatus string

const (
	CommandStatusAccepted CommandStatus = "accepted"
	CommandStatusRejected CommandStatus = "rejected"
)

// CommandDisposition 描述已接受命令的控制面动作。
type CommandDisposition string

const (
	CommandDispositionQueried      CommandDisposition = "queried"
	CommandDispositionCompacting   CommandDisposition = "compacting"
	CommandDispositionCancelling   CommandDisposition = "cancelling"
	CommandDispositionRetracted    CommandDisposition = "retracted"
	CommandDispositionStopping     CommandDisposition = "stopping"
	CommandDispositionStarting     CommandDisposition = "starting"
	CommandDispositionQueued       CommandDisposition = "queued"
	CommandDispositionInterrupting CommandDisposition = "interrupting"
	CommandDispositionClosing      CommandDisposition = "closing"
	CommandDispositionResolving    CommandDisposition = "resolving"
)

// ControlErrorCode 是可以安全投影到宿主协议的稳定错误码。
type ControlErrorCode string

const (
	ControlErrorSessionBusy              ControlErrorCode = "session_busy"
	ControlErrorNothingToCompact         ControlErrorCode = "nothing_to_compact"
	ControlErrorCompactionBudgetUnknown  ControlErrorCode = "compaction_budget_unknown"
	ControlErrorCompactionBudgetExceeded ControlErrorCode = "compaction_budget_exceeded"
	ControlErrorNoActiveCompaction       ControlErrorCode = "no_active_compaction"
	ControlErrorCompactionMismatch       ControlErrorCode = "compaction_mismatch"
	ControlErrorInputNotQueued           ControlErrorCode = "input_not_queued"
	ControlErrorInputEncodingLimit       ControlErrorCode = "input_encoding_limit"
	ControlErrorInputStopping            ControlErrorCode = "input_stopping"
	ControlErrorInvalidCommand           ControlErrorCode = "invalid_command"
	ControlErrorInvalidInput             ControlErrorCode = "invalid_input"
	ControlErrorInputQueueFull           ControlErrorCode = "input_queue_full"
	ControlErrorDuplicateRequest         ControlErrorCode = "duplicate_request"
	ControlErrorNoActiveTurn             ControlErrorCode = "no_active_turn"
	ControlErrorTurnMismatch             ControlErrorCode = "turn_mismatch"
	ControlErrorNoActiveApproval         ControlErrorCode = "no_active_approval"
	ControlErrorApprovalMismatch         ControlErrorCode = "approval_mismatch"
	ControlErrorSessionClosing           ControlErrorCode = "session_closing"
	ControlErrorSessionShutdown          ControlErrorCode = "session_shutdown"
	ControlErrorSessionFailed            ControlErrorCode = "session_failed"
)

// ControlError 是不包含底层 cause 的控制面安全错误摘要。
type ControlError struct {
	code    ControlErrorCode
	message string
}

// Code 返回稳定错误码。
func (controlError ControlError) Code() ControlErrorCode { return controlError.code }

// Message 返回固定英文安全消息。
func (controlError ControlError) Message() string { return controlError.message }

// Validate 验证错误码与固定安全消息匹配。
func (controlError ControlError) Validate() error {
	message, ok := controlErrorMessage(controlError.code)
	if !ok || message != controlError.message {
		return fmt.Errorf("control error is invalid")
	}
	return nil
}

// CommandResult 是 owner 对一条命令的确定 admission 结果。
type CommandResult struct {
	compactionID domain.CompactionID
	entryID      domain.InputID
	requestID    RequestID
	commandKind  CommandKind
	status       CommandStatus
	disposition  CommandDisposition
	controlErr   ControlError
}

// NewAcceptedCommandResult 创建与 command kind 匹配的接受结果。
func NewAcceptedCommandResult(command Command, disposition CommandDisposition) (CommandResult, error) {
	if err := command.Validate(); err != nil {
		return CommandResult{}, fmt.Errorf("accepted command is invalid")
	}
	result := CommandResult{
		requestID: command.RequestID(), commandKind: command.Kind(),
		status: CommandStatusAccepted, disposition: disposition,
	}
	if err := result.Validate(); err != nil {
		return CommandResult{}, err
	}
	return result, nil
}

// NewRejectedCommandResult 创建只含固定安全摘要的拒绝结果。
func NewRejectedCommandResult(command Command, code ControlErrorCode) (CommandResult, error) {
	if err := command.Validate(); err != nil {
		return CommandResult{}, fmt.Errorf("rejected command is invalid")
	}
	message, ok := controlErrorMessage(code)
	if !ok {
		return CommandResult{}, fmt.Errorf("control error code is invalid")
	}
	result := CommandResult{
		requestID: command.RequestID(), commandKind: command.Kind(), status: CommandStatusRejected,
		controlErr: ControlError{code: code, message: message},
	}
	if err := result.Validate(); err != nil {
		return CommandResult{}, err
	}
	return result, nil
}

// RequestID 返回原命令关联标识。
func (result CommandResult) RequestID() RequestID { return result.requestID }

// CommandKind 返回原命令类型。
func (result CommandResult) CommandKind() CommandKind { return result.commandKind }

// Status 返回接受或拒绝状态。
func (result CommandResult) Status() CommandStatus { return result.status }

// Disposition 返回已接受命令的动作；拒绝结果返回零值。
func (result CommandResult) Disposition() CommandDisposition { return result.disposition }

// Error 返回拒绝摘要及存在标记。
func (result CommandResult) Error() (ControlError, bool) {
	return result.controlErr, result.status == CommandStatusRejected
}

// Validate 验证 command kind、status 和 disposition/error 的组合。
func (result CommandResult) Validate() error {
	if result.commandKind == CommandCompactContext && result.status == CommandStatusAccepted {
		if !result.compactionID.Valid() {
			return fmt.Errorf("accepted compaction identity is invalid")
		}
	} else if result.compactionID != "" {
		return fmt.Errorf("unrelated compaction identity")
	}

	if !result.requestID.Valid() || !validCommandKind(result.commandKind) {
		return fmt.Errorf("command result identity is invalid")
	}
	switch result.status {
	case CommandStatusAccepted:
		if result.commandKind == CommandSubmitConversationalInput {
			if !result.entryID.Valid() {
				return fmt.Errorf("accepted input identity is invalid")
			}
		} else if result.entryID != "" {
			return fmt.Errorf("unrelated input identity")
		}
		if result.controlErr != (ControlError{}) || !dispositionMatches(result.commandKind, result.disposition) {
			return fmt.Errorf("accepted command result is invalid")
		}
	case CommandStatusRejected:
		if result.entryID != "" || result.disposition != "" || result.controlErr.Validate() != nil {
			return fmt.Errorf("rejected command result is invalid")
		}
	default:
		return fmt.Errorf("command result status is invalid")
	}
	return nil
}

// ControlItemKind 描述 owner 向宿主发布的有序输出类型。
type ControlItemKind string

const (
	ControlItemContextEvent  ControlItemKind = "context_event"
	ControlItemInputStatus   ControlItemKind = "input_status"
	ControlItemCommandResult ControlItemKind = "command_result"
	ControlItemTurnEvent     ControlItemKind = "turn_event"
	ControlItemInputDiscard  ControlItemKind = "input_discard"
)

// ControlItem 是 command result、关联 turn event 与 input discard 的封闭联合。
type ControlItem struct {
	inputStatus InputStatus
	kind        ControlItemKind
	requestID   RequestID
	result      CommandResult
	event       Event
	discard     ControlError
}

// NewCommandResultItem 创建命令结果输出。
func NewCommandResultItem(result CommandResult) (ControlItem, error) {
	item := ControlItem{kind: ControlItemCommandResult, requestID: result.RequestID(), result: result}
	if err := item.Validate(); err != nil {
		return ControlItem{}, err
	}
	return item, nil
}

// NewCorrelatedTurnItem 创建携带 input identity 的 RuntimeEvent 输出。
func NewCorrelatedTurnItem(requestID RequestID, event Event) (ControlItem, error) {
	item := ControlItem{kind: ControlItemTurnEvent, requestID: requestID, event: event}
	if err := item.Validate(); err != nil {
		return ControlItem{}, err
	}
	return item, nil
}

// NewInputDiscardItem 创建尚未启动输入的明确终止输出。
func NewInputDiscardItem(requestID RequestID, code ControlErrorCode) (ControlItem, error) {
	message, ok := controlErrorMessage(code)
	if !ok || (code != ControlErrorSessionShutdown && code != ControlErrorSessionFailed) {
		return ControlItem{}, fmt.Errorf("input discard reason is invalid")
	}
	item := ControlItem{
		kind: ControlItemInputDiscard, requestID: requestID,
		discard: ControlError{code: code, message: message},
	}
	if err := item.Validate(); err != nil {
		return ControlItem{}, err
	}
	return item, nil
}

// Kind 返回 control item 类型。
func (item ControlItem) Kind() ControlItemKind { return item.kind }

// RequestID 返回关联的 command 或 input identity。
func (item ControlItem) RequestID() RequestID { return item.requestID }

// CommandResult 返回命令结果及存在标记。
func (item ControlItem) CommandResult() (CommandResult, bool) {
	return item.result, item.kind == ControlItemCommandResult
}

// TurnEvent 返回 RuntimeEvent 及存在标记。
func (item ControlItem) TurnEvent() (Event, bool) {
	return item.event, item.kind == ControlItemTurnEvent
}

// DiscardError 返回 input discard 摘要及存在标记。
func (item ControlItem) DiscardError() (ControlError, bool) {
	return item.discard, item.kind == ControlItemInputDiscard
}

// Validate 验证封闭联合只有一个与 kind 匹配的 payload。
func (item ControlItem) Validate() error {
	if !item.requestID.Valid() {
		return fmt.Errorf("control item request ID is invalid")
	}
	if item.kind != ControlItemInputStatus && item.inputStatus != (InputStatus{}) {
		return fmt.Errorf("unrelated input status")
	}
	switch item.kind {
	case ControlItemInputStatus:
		if item.inputStatus.Validate() != nil || item.inputStatus.RequestID() != item.requestID || item.result != (CommandResult{}) || item.event.Kind != "" || item.discard != (ControlError{}) {
			return fmt.Errorf("input status item is invalid")
		}
	case ControlItemCommandResult:
		if item.result.Validate() != nil || item.result.RequestID() != item.requestID || item.event.Kind != "" || item.discard != (ControlError{}) {
			return fmt.Errorf("command result item is invalid")
		}
	case ControlItemContextEvent:
		if item.result != (CommandResult{}) || item.discard != (ControlError{}) || item.event.TurnID != "" || item.event.Validate() != nil || (item.event.Kind != EventContextSnapshot && item.event.Kind != EventCompactionStarted && item.event.Kind != EventCompactionCompleted && item.event.Kind != EventCompactionFailed) {
			return fmt.Errorf("correlated context item is invalid")
		}
	case ControlItemTurnEvent:
		if item.result != (CommandResult{}) || item.discard != (ControlError{}) || validateRuntimeEvent(item.event) != nil {
			return fmt.Errorf("correlated turn item is invalid")
		}
	case ControlItemInputDiscard:
		if item.result != (CommandResult{}) || item.event.Kind != "" || item.discard.Validate() != nil ||
			(item.discard.code != ControlErrorSessionShutdown && item.discard.code != ControlErrorSessionFailed) {
			return fmt.Errorf("input discard item is invalid")
		}
	default:
		return fmt.Errorf("control item kind is invalid")
	}
	return nil
}

func validCommandKind(kind CommandKind) bool {
	return kind == CommandQueryContext || kind == CommandCompactContext || kind == CommandCancelCompaction || kind == CommandSubmitConversationalInput || kind == CommandRetractInputs || kind == CommandStopTurn || kind == CommandSubmitInput || kind == CommandInterrupt || kind == CommandShutdown || kind == CommandResolveToolApproval
}

func dispositionMatches(kind CommandKind, disposition CommandDisposition) bool {
	switch kind {
	case CommandQueryContext:
		return disposition == CommandDispositionQueried
	case CommandCompactContext:
		return disposition == CommandDispositionCompacting
	case CommandCancelCompaction:
		return disposition == CommandDispositionCancelling
	case CommandSubmitInput, CommandSubmitConversationalInput:
		return disposition == CommandDispositionStarting || disposition == CommandDispositionQueued
	case CommandRetractInputs:
		return disposition == CommandDispositionRetracted
	case CommandStopTurn:
		return disposition == CommandDispositionStopping
	case CommandInterrupt:
		return disposition == CommandDispositionInterrupting
	case CommandShutdown:
		return disposition == CommandDispositionClosing
	case CommandResolveToolApproval:
		return disposition == CommandDispositionResolving
	default:
		return false
	}
}

func controlErrorMessage(code ControlErrorCode) (string, bool) {
	switch code {
	case ControlErrorSessionBusy:
		return "session has active or queued work", true
	case ControlErrorNothingToCompact:
		return "there is no new context to compact", true
	case ControlErrorCompactionBudgetUnknown:
		return "compaction budget cannot be estimated", true
	case ControlErrorCompactionBudgetExceeded:
		return "compaction request exceeds the context budget", true
	case ControlErrorNoActiveCompaction:
		return "there is no active compaction", true
	case ControlErrorCompactionMismatch:
		return "active compaction does not match the expected identity", true
	case ControlErrorInputNotQueued:
		return "input is no longer queued", true
	case ControlErrorInputEncodingLimit:
		return "input exceeds encoding limits", true
	case ControlErrorInputStopping:
		return "automatic input is unavailable while stopping", true
	case ControlErrorInvalidCommand:
		return "command is invalid", true
	case ControlErrorInvalidInput:
		return "input is invalid", true
	case ControlErrorInputQueueFull:
		return "input queue is full", true
	case ControlErrorDuplicateRequest:
		return "request ID was already used", true
	case ControlErrorNoActiveTurn:
		return "there is no active turn", true
	case ControlErrorTurnMismatch:
		return "active turn does not match the expected turn", true
	case ControlErrorNoActiveApproval:
		return "there is no active tool approval", true
	case ControlErrorApprovalMismatch:
		return "active tool approval does not match the response", true
	case ControlErrorSessionClosing:
		return "session is closing", true
	case ControlErrorSessionShutdown:
		return "input was discarded because the session is shutting down", true
	case ControlErrorSessionFailed:
		return "input was discarded because the session cannot continue", true
	default:
		return "", false
	}
}

func validateRuntimeEvent(event Event) error {
	if !event.SessionID.Valid() || !event.ThreadID.Valid() || !event.TurnID.Valid() {
		return fmt.Errorf("runtime event identity is invalid")
	}
	return event.Validate()
}

// NewAcceptedCompactionResult 创建带独立操作身份的 admission 结果。
func NewAcceptedCompactionResult(command Command, id domain.CompactionID) (CommandResult, error) {
	if command.Validate() != nil || command.Kind() != CommandCompactContext {
		return CommandResult{}, fmt.Errorf("compaction command is invalid")
	}
	result := CommandResult{requestID: command.RequestID(), commandKind: command.Kind(), status: CommandStatusAccepted, disposition: CommandDispositionCompacting, compactionID: id}
	return result, result.Validate()
}

// CompactionID 返回接受压缩时分配的独立身份。
func (result CommandResult) CompactionID() domain.CompactionID { return result.compactionID }

// NewCorrelatedContextItem 创建 thread 查询或压缩事件的关联输出。
func NewCorrelatedContextItem(requestID RequestID, event Event) (ControlItem, error) {
	event.Payload = append([]byte(nil), event.Payload...)
	item := ControlItem{kind: ControlItemContextEvent, requestID: requestID, event: event}
	return item, item.Validate()
}

// ContextEvent 返回独立复制的查询或压缩事件。
func (item ControlItem) ContextEvent() (Event, bool) {
	if item.kind != ControlItemContextEvent {
		return Event{}, false
	}
	event := item.event
	event.Payload = append([]byte(nil), event.Payload...)
	return event, true
}
