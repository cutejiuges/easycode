package protocol

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const maxApprovalDiffBytes = 256 << 10

// ApprovalDecision 是宿主可提交的封闭批准决定。
type ApprovalDecision string

const (
	ApprovalAllowOnce    ApprovalDecision = "allow_once"
	ApprovalAllowSession ApprovalDecision = "allow_session"
	ApprovalDeny         ApprovalDecision = "deny"
	ApprovalAbort        ApprovalDecision = "abort"
)

func (decision ApprovalDecision) Valid() bool {
	return decision == ApprovalAllowOnce || decision == ApprovalAllowSession || decision == ApprovalDeny || decision == ApprovalAbort
}

// ToolApprovalRequestedPayload 保存不含Provider wire与完整文件正文的批准视图。
type ToolApprovalRequestedPayload struct {
	RequestID      string                `json:"request_id"`
	InvocationID   string                `json:"invocation_id"`
	ProviderCallID string                `json:"provider_call_id"`
	Capability     string                `json:"capability"`
	Reason         string                `json:"reason"`
	RelativePath   string                `json:"relative_path"`
	Fingerprint    string                `json:"fingerprint"`
	Presentation   *ApprovalPresentation `json:"presentation,omitempty"`
	Decisions      []ApprovalDecision    `json:"decisions"`
	Command        string                `json:"command,omitempty"`
	TimeoutMillis  int                   `json:"timeout_ms,omitempty"`
}

// ToolApprovalResolvedPayload 通知宿主关闭匹配overlay。
type ToolApprovalResolvedPayload struct {
	RequestID    string           `json:"request_id"`
	InvocationID string           `json:"invocation_id"`
	Fingerprint  string           `json:"fingerprint"`
	Decision     ApprovalDecision `json:"decision"`
}

// AssistantTextDeltaPayload 保存一次 assistant 文本增量。
type AssistantTextDeltaPayload struct {
	Text string `json:"text"`
}

// TurnFailedPayload 保存可供宿主安全展示的 turn 失败摘要。
type TurnFailedPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// TurnCompletedPayload 保存成功 turn 的归一化 usage。
type TurnCompletedPayload struct {
	Usage UsagePayload `json:"usage"`
}

func newEventWithPayload(kind EventKind, payload any) (Event, error) {
	encoded, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal runtime event payload: %w", err)
	}
	event := newEvent(kind)
	event.Payload = encoded
	return event, nil
}

// NewTurnStarted 创建 turn 开始事件。
func NewTurnStarted() Event {
	return newEvent(EventTurnStarted)
}

// ValidateTurnStarted 严格校验 turn 开始事件。
func ValidateTurnStarted(event Event) error {
	return validatePayloadlessEvent(event, EventTurnStarted)
}

// NewAssistantTextDelta 创建 assistant 文本增量事件。
func NewAssistantTextDelta(text string) (Event, error) {
	if text == "" {
		return Event{}, fmt.Errorf("assistant text delta is required")
	}
	return newEventWithPayload(EventAssistantTextDelta, AssistantTextDeltaPayload{Text: text})
}

// DecodeAssistantTextDelta 解码并校验 assistant 文本增量。
func DecodeAssistantTextDelta(event Event) (AssistantTextDeltaPayload, error) {
	if event.Kind != EventAssistantTextDelta {
		return AssistantTextDeltaPayload{}, fmt.Errorf("event kind is not assistant_text_delta")
	}
	var payload AssistantTextDeltaPayload
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return AssistantTextDeltaPayload{}, fmt.Errorf("decode assistant text delta: %w", err)
	}
	if payload.Text == "" {
		return AssistantTextDeltaPayload{}, fmt.Errorf("assistant text delta is required")
	}
	return payload, nil
}

// NewTurnFailed 创建安全的 turn 失败事件。
func NewTurnFailed(code string, message string, cancelled bool) (Event, error) {
	if strings.TrimSpace(code) == "" {
		return Event{}, fmt.Errorf("turn failure code is required")
	}
	if strings.TrimSpace(message) == "" {
		return Event{}, fmt.Errorf("turn failure message is required")
	}
	return newEventWithPayload(EventTurnFailed, TurnFailedPayload{
		Code:      code,
		Message:   message,
		Cancelled: cancelled,
	})
}

// DecodeTurnFailed 解码并校验 turn 失败事件。
func DecodeTurnFailed(event Event) (TurnFailedPayload, error) {
	if event.Kind != EventTurnFailed {
		return TurnFailedPayload{}, fmt.Errorf("event kind is not turn_failed")
	}
	var payload TurnFailedPayload
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return TurnFailedPayload{}, fmt.Errorf("decode turn failure: %w", err)
	}
	if strings.TrimSpace(payload.Code) == "" || strings.TrimSpace(payload.Message) == "" {
		return TurnFailedPayload{}, fmt.Errorf("turn failure payload is incomplete")
	}
	return payload, nil
}

// NewTurnCompleted 创建携带 turn usage 的成功完成事件。
func NewTurnCompleted(usage domain.SampleUsage) (Event, error) {
	payload, err := NewUsagePayload(usage)
	if err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventTurnCompleted, TurnCompletedPayload{Usage: payload})
}

// DecodeTurnCompleted 解码并校验 turn 成功完成事件。
func DecodeTurnCompleted(event Event) (TurnCompletedPayload, error) {
	if event.Kind != EventTurnCompleted {
		return TurnCompletedPayload{}, fmt.Errorf("event kind is not turn_completed")
	}
	var payload TurnCompletedPayload
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return TurnCompletedPayload{}, fmt.Errorf("decode turn completion: %w", err)
	}
	if _, err := payload.Usage.Domain(); err != nil {
		return TurnCompletedPayload{}, fmt.Errorf("turn completion usage is invalid: %w", err)
	}
	return payload, nil
}

// NewToolApprovalRequested 创建绑定精确diff的批准请求事件。
func NewToolApprovalRequested(payload ToolApprovalRequestedPayload) (Event, error) {
	if err := validateApprovalRequested(payload); err != nil {
		return Event{}, err
	}
	payload.Decisions = append([]ApprovalDecision(nil), payload.Decisions...)
	return newEventWithPayload(EventToolApprovalRequested, payload)
}
func DecodeToolApprovalRequested(event Event) (ToolApprovalRequestedPayload, error) {
	if event.Kind != EventToolApprovalRequested {
		return ToolApprovalRequestedPayload{}, fmt.Errorf("event kind is not tool_approval_requested")
	}
	var payload ToolApprovalRequestedPayload
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return ToolApprovalRequestedPayload{}, fmt.Errorf("decode tool approval request: %w", err)
	}
	if err := validateApprovalRequested(payload); err != nil {
		return ToolApprovalRequestedPayload{}, err
	}
	payload.Decisions = append([]ApprovalDecision(nil), payload.Decisions...)
	return payload, nil
}

func validateApprovalRequested(payload ToolApprovalRequestedPayload) error {
	if !validUUIDv7Text(payload.RequestID) || !validUUIDv7Text(payload.InvocationID) || payload.ProviderCallID == "" || len(payload.ProviderCallID) > 256 || strings.TrimSpace(payload.Reason) == "" || len(payload.Reason) > 1024 || !validFingerprint(payload.Fingerprint) {
		return fmt.Errorf("tool approval request payload is invalid")
	}
	if payload.Capability == "process.exec" {
		if payload.Presentation != nil || payload.Command == "" || len(payload.Command) > 64<<10 || !utf8.ValidString(payload.Command) || strings.ContainsRune(payload.Command, 0) || payload.TimeoutMillis < 1000 || payload.TimeoutMillis > 600000 || (payload.RelativePath != "" && !validApprovalPath(payload.RelativePath)) || len(payload.Decisions) != 3 || payload.Decisions[0] != ApprovalAllowOnce || payload.Decisions[1] != ApprovalDeny || payload.Decisions[2] != ApprovalAbort {
			return fmt.Errorf("invalid Exec approval payload")
		}
		return nil
	}
	if payload.Command != "" || payload.TimeoutMillis != 0 {
		return fmt.Errorf("mismatched approval payload")
	}
	if payload.Presentation == nil || payload.Capability != payload.Presentation.Capability() || !validApprovalPath(payload.RelativePath) || !utf8.ValidString(payload.Reason) || payload.Presentation.Validate() != nil {
		return fmt.Errorf("tool approval request payload is invalid")
	}
	want := []ApprovalDecision{ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny}
	if len(payload.Decisions) != len(want) {
		return fmt.Errorf("tool approval decisions are invalid")
	}
	for i := range want {
		if payload.Decisions[i] != want[i] {
			return fmt.Errorf("tool approval decisions are invalid")
		}
	}
	return nil
}

func validApprovalPath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

// NewToolApprovalResolved 创建批准请求终止事件。
func NewToolApprovalResolved(payload ToolApprovalResolvedPayload) (Event, error) {
	if err := validateApprovalResolved(payload); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventToolApprovalResolved, payload)
}
func DecodeToolApprovalResolved(event Event) (ToolApprovalResolvedPayload, error) {
	if event.Kind != EventToolApprovalResolved {
		return ToolApprovalResolvedPayload{}, fmt.Errorf("event kind is not tool_approval_resolved")
	}
	var payload ToolApprovalResolvedPayload
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return ToolApprovalResolvedPayload{}, fmt.Errorf("decode tool approval resolution: %w", err)
	}
	return payload, validateApprovalResolved(payload)
}
func validateApprovalResolved(payload ToolApprovalResolvedPayload) error {
	if !validUUIDv7Text(payload.RequestID) || !validUUIDv7Text(payload.InvocationID) || !validFingerprint(payload.Fingerprint) || !payload.Decision.Valid() {
		return fmt.Errorf("tool approval resolution payload is invalid")
	}
	return nil
}

func validUUIDv7Text(value string) bool { _, err := domain.ParseTurnID(value); return err == nil }
func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validatePayloadlessEvent(event Event, kind EventKind) error {
	if event.Kind != kind {
		return fmt.Errorf("event kind is not %s", kind)
	}
	if len(event.Payload) != 0 {
		return fmt.Errorf("%s payload must be empty", kind)
	}
	return nil
}
