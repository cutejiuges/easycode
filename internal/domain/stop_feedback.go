package domain

import (
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	MaxStopContinuations     = 2
	MaxStopFeedbackBytes     = 8 << 10
	MaxTurnStopFeedbackBytes = 16 << 10
	stopFeedbackPrefix       = "<completion_check_feedback>\nCompletion check data follows. Existing user instructions and tool permissions still apply.\n"
	stopFeedbackSuffix       = "\n</completion_check_feedback>"
)

type stopFeedbackBody struct {
	Source HookSource `json:"source"`
	ID     string     `json:"id"`
	Reason string     `json:"reason"`
}

// StopFeedback 保存一次确定的模型正文及真实来源；恢复只能复用最终字节。
type StopFeedback struct {
	Identity  HookRunIdentity `json:"identity"`
	Reference HookReference   `json:"reference"`
	Text      string          `json:"text"`
	Bytes     int             `json:"bytes"`
	Digest    string          `json:"digest"`
}

func NewStopFeedback(result HookRunResult, ref HookReference, candidate HookFeedbackCandidate) (StopFeedback, error) {
	if result.Validate() != nil || result.Identity.Event != HookStop || result.Status != HookBlock || result.Code != HookDenied || !result.ExitKnown || result.ExitCode != 0 || !result.CleanupConfirmed || result.Cancelled || result.TimedOut || result.Origin != "execution" || ref.Validate() != nil || ref.Event != HookStop || ref.Feedback != HookFeedbackModel || candidate.Empty() || candidate.Validate() != nil {
		return StopFeedback{}, HookConfigInvalid
	}
	body, err := codec.MarshalStable(stopFeedbackBody{ref.Source, ref.ID, candidate.Content()})
	if err != nil {
		return StopFeedback{}, HookFeedbackEncodingFailed
	}
	text := stopFeedbackPrefix + string(body) + stopFeedbackSuffix
	if len(text) > MaxStopFeedbackBytes {
		return StopFeedback{}, HookFeedbackBudgetExceeded
	}
	v := StopFeedback{result.Identity, ref, text, len(text), HookDigest([]byte(text))}
	return v, v.Validate()
}
func (v StopFeedback) Empty() bool { return v == (StopFeedback{}) }
func (v StopFeedback) Validate() error {
	if v.Identity.Validate() != nil || v.Identity.Event != HookStop || v.Reference.Validate() != nil || v.Reference.Event != HookStop || v.Reference.Feedback != HookFeedbackModel || len(v.Text) > MaxStopFeedbackBytes || v.Bytes != len(v.Text) || !utf8.ValidString(v.Text) || v.Digest != HookDigest([]byte(v.Text)) || !strings.HasPrefix(v.Text, stopFeedbackPrefix) || !strings.HasSuffix(v.Text, stopFeedbackSuffix) {
		return HookConfigInvalid
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(v.Text, stopFeedbackPrefix), stopFeedbackSuffix)
	var body stopFeedbackBody
	if codec.DecodeObject([]byte(inner), &body, "source", "id", "reason") != nil || body.Source != v.Reference.Source || body.ID != v.Reference.ID {
		return HookConfigInvalid
	}
	c, err := NewHookFeedbackCandidate(body.Reason)
	if err != nil || c.Empty() {
		return HookConfigInvalid
	}
	return nil
}
func DecodeStopFeedback(b []byte) (StopFeedback, error) {
	var v StopFeedback
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &v) != nil {
		return StopFeedback{}, HookConfigInvalid
	}
	return v, v.Validate()
}
