package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const (
	CommandResolveUserQuestion        CommandKind      = "resolve_user_question"
	EventUserQuestionRequested        EventKind        = "user_question_requested"
	EventUserQuestionResolved         EventKind        = "user_question_resolved"
	ControlErrorNoActiveQuestion      ControlErrorCode = "no_active_question"
	ControlErrorQuestionMismatch      ControlErrorCode = "question_mismatch"
	ControlErrorQuestionAnswerInvalid ControlErrorCode = "question_answer_invalid"
)

// UserQuestionRequestedPayload 是与单个工具 slot 绑定的有界问卷。
type UserQuestionRequestedPayload struct {
	RequestID       domain.QuestionRequestID `json:"request_id"`
	InvocationID    string                   `json:"invocation_id"`
	Fingerprint     string                   `json:"fingerprint"`
	Input           QuestionInput            `json:"input"`
	ResultAllowance int                      `json:"result_allowance"`
}

func (p UserQuestionRequestedPayload) Validate() error {
	if !p.RequestID.Valid() || !validUUIDv7Text(p.InvocationID) || !validFingerprint(p.Fingerprint) || p.Input.Validate() != nil || p.Fingerprint != p.Input.Fingerprint() || p.ResultAllowance < 1024 || p.ResultAllowance > MaxQuestionPreviewBytes {
		return fmt.Errorf("invalid user question request")
	}
	return nil
}

// QuestionResolution 仅表达已经持久化的问卷结果。
type QuestionResolution string

const (
	QuestionAnswered  QuestionResolution = "answered"
	QuestionSkipped   QuestionResolution = "skipped"
	QuestionCancelled QuestionResolution = "cancelled"
)

type UserQuestionResolvedPayload struct {
	RequestID    domain.QuestionRequestID `json:"request_id"`
	InvocationID string                   `json:"invocation_id"`
	Fingerprint  string                   `json:"fingerprint"`
	Resolution   QuestionResolution       `json:"resolution"`
}

func (p UserQuestionResolvedPayload) Validate() error {
	if !p.RequestID.Valid() || !validUUIDv7Text(p.InvocationID) || !validFingerprint(p.Fingerprint) || (p.Resolution != QuestionAnswered && p.Resolution != QuestionSkipped && p.Resolution != QuestionCancelled) {
		return fmt.Errorf("invalid question resolution")
	}
	return nil
}
func NewUserQuestionRequested(p UserQuestionRequestedPayload) (Event, error) {
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventUserQuestionRequested, p)
}
func DecodeUserQuestionRequested(e Event) (UserQuestionRequestedPayload, error) {
	var p UserQuestionRequestedPayload
	if e.Kind != EventUserQuestionRequested || codec.UnmarshalRecord(e.Payload, &p) != nil || p.Validate() != nil {
		return UserQuestionRequestedPayload{}, fmt.Errorf("invalid question request event")
	}
	return p, nil
}
func NewUserQuestionResolved(p UserQuestionResolvedPayload) (Event, error) {
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventUserQuestionResolved, p)
}
func DecodeUserQuestionResolved(e Event) (UserQuestionResolvedPayload, error) {
	var p UserQuestionResolvedPayload
	if e.Kind != EventUserQuestionResolved || codec.UnmarshalRecord(e.Payload, &p) != nil || p.Validate() != nil {
		return UserQuestionResolvedPayload{}, fmt.Errorf("invalid question resolution event")
	}
	return p, nil
}

// UserQuestionResponse 是唯一当前的答案控制值，不能包含授权或新用户输入。
type UserQuestionResponse struct {
	RequestID    domain.QuestionRequestID `json:"request_id"`
	InvocationID string                   `json:"invocation_id"`
	Fingerprint  string                   `json:"fingerprint"`
	Answers      QuestionAnswerSet        `json:"answers"`
}

func NewUserQuestionResponse(requestID domain.QuestionRequestID, invocationID, fingerprint string, answers QuestionAnswerSet) (UserQuestionResponse, error) {
	p := UserQuestionResponse{requestID, invocationID, fingerprint, answers}
	return p, p.Validate()
}
func (p UserQuestionResponse) Validate() error {
	if !p.RequestID.Valid() || !validUUIDv7Text(p.InvocationID) || !validFingerprint(p.Fingerprint) || p.Answers.Validate() != nil {
		return fmt.Errorf("invalid question response")
	}
	return nil
}
func DecodeUserQuestionResponse(data []byte) (UserQuestionResponse, error) {
	var p UserQuestionResponse
	if len(data) > MaxQuestionPreviewBytes+1024 || codec.UnmarshalRecord(data, &p) != nil || p.Validate() != nil {
		return UserQuestionResponse{}, fmt.Errorf("invalid question response")
	}
	return p, nil
}
func NewResolveUserQuestionCommand(id RequestID, turnID domain.TurnID, response UserQuestionResponse) (Command, error) {
	c := Command{kind: CommandResolveUserQuestion, requestID: id, expectedTurnID: turnID, questionResponse: response}
	return c, c.Validate()
}
func (c Command) QuestionResponse() UserQuestionResponse { return c.questionResponse }
func DecodeResolveUserQuestionCommand(data []byte) (Command, error) {
	var w struct {
		RequestID RequestID            `json:"request_id"`
		TurnID    domain.TurnID        `json:"turn_id"`
		Response  UserQuestionResponse `json:"response"`
	}
	if len(data) > MaxQuestionPreviewBytes+2048 || codec.UnmarshalRecord(data, &w) != nil {
		return Command{}, fmt.Errorf("invalid question command")
	}
	return NewResolveUserQuestionCommand(w.RequestID, w.TurnID, w.Response)
}
