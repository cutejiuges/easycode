package tool

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"easycode/internal/codec"
)

// QuestionResultMetadata 只保存关联和计数，答案正文仅保存于冻结预览。
type QuestionResultMetadata struct {
	Fingerprint   string `json:"fingerprint"`
	QuestionCount int    `json:"question_count"`
	AnsweredCount int    `json:"answered_count"`
	SkippedCount  int    `json:"skipped_count"`
}

type questionPreviewWire struct {
	Answers        []QuestionAnswer `json:"answers"`
	SelectedLabels [][]string       `json:"selected_labels"`
}

// ValidateQuestionResultShape 在读取单条事实时验证自含结构；题目关联由后续 replay 校验。
func ValidateQuestionResultShape(status ResultStatus, code string, preview ModelPreview, metadata QuestionResultMetadata, budget ResultBudgetMetadata) error {
	digest, err := hex.DecodeString(metadata.Fingerprint)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != metadata.Fingerprint || metadata.QuestionCount < 1 || metadata.QuestionCount > 4 || metadata.AnsweredCount < 0 || metadata.SkippedCount < 0 || !status.Valid() || !validResultCode(code) || code == "" || preview.Validate() != nil || budget.Validate() != nil || budget.Allowance() > MaxQuestionPreviewBytes || budget.ActualBytes() != len(preview.Text()) {
		return fmt.Errorf("invalid question result shape")
	}
	if status != ResultSuccess {
		if metadata.AnsweredCount != 0 || metadata.SkippedCount != 0 {
			return fmt.Errorf("unsuccessful question contains answers")
		}
		return nil
	}
	if code != "ok" || metadata.AnsweredCount+metadata.SkippedCount != metadata.QuestionCount || budget.Reason() != BudgetPreserved {
		return fmt.Errorf("invalid successful question result")
	}
	var w questionPreviewWire
	if codec.UnmarshalRecord([]byte(preview.Text()), &w) != nil {
		return fmt.Errorf("invalid question result preview")
	}
	as, err := NewQuestionAnswerSet(w.Answers)
	if err != nil || len(w.Answers) != metadata.QuestionCount || len(w.SelectedLabels) != len(w.Answers) {
		return fmt.Errorf("invalid question result answers")
	}
	skipped := 0
	for i, a := range as.Answers() {
		if a.Skipped {
			skipped++
		}
		if len(w.SelectedLabels[i]) != len(a.SelectedOptions) {
			return fmt.Errorf("invalid selected labels")
		}
		for _, label := range w.SelectedLabels[i] {
			if len(label) == 0 || len(label) > 128 {
				return fmt.Errorf("invalid selected label")
			}
		}
	}
	if skipped != metadata.SkippedCount {
		return fmt.Errorf("invalid skipped answer count")
	}
	return nil
}

func encodeQuestionPreview(input QuestionInput, answers QuestionAnswerSet) ([]byte, error) {
	if err := answers.ValidateAgainst(input); err != nil {
		return nil, err
	}
	qs, as := input.Questions(), answers.Answers()
	labels := make([][]string, len(qs))
	for i, a := range as {
		labels[i] = []string{}
		for _, index := range a.SelectedOptions {
			labels[i] = append(labels[i], qs[i].Options[index].Label)
		}
	}
	return codec.MarshalStable(questionPreviewWire{as, labels})
}

// DecodeQuestionPreview 验证完整答案和原始选项标签一致，不接受部分成功结果。
func DecodeQuestionPreview(input QuestionInput, data []byte) (QuestionAnswerSet, error) {
	if len(data) > MaxQuestionPreviewBytes {
		return QuestionAnswerSet{}, fmt.Errorf("question result exceeds limit")
	}
	var w questionPreviewWire
	if codec.UnmarshalRecord(data, &w) != nil {
		return QuestionAnswerSet{}, fmt.Errorf("invalid question result")
	}
	a, err := NewQuestionAnswerSet(w.Answers)
	if err != nil {
		return QuestionAnswerSet{}, err
	}
	canonical, err := encodeQuestionPreview(input, a)
	if err != nil || !bytes.Equal(canonical, data) {
		return QuestionAnswerSet{}, fmt.Errorf("question result does not match input")
	}
	return a, nil
}

func NewQuestionInvocationResult(v QuestionInvocation, answers QuestionAnswerSet) (InvocationResult, error) {
	if err := v.Validate(); err != nil {
		return InvocationResult{}, err
	}
	b, err := encodeQuestionPreview(v.input, answers)
	if err != nil || len(b) > MaxQuestionPreviewBytes {
		return InvocationResult{}, fmt.Errorf("question result exceeds limit")
	}
	p, err := NewModelPreview(string(b))
	if err != nil {
		return InvocationResult{}, err
	}
	m := QuestionResultMetadata{Fingerprint: v.input.Fingerprint(), QuestionCount: len(v.input.Questions())}
	for _, a := range answers.Answers() {
		if a.Skipped {
			m.SkippedCount++
		} else {
			m.AnsweredCount++
		}
	}
	return RestoreQuestionInvocationResult(v, ResultSuccess, "ok", p, m)
}

// RestoreQuestionInvocationResult 仅从当前冻结事实重建结果，不执行交互。
func RestoreQuestionInvocationResult(v QuestionInvocation, status ResultStatus, code string, p ModelPreview, m QuestionResultMetadata) (InvocationResult, error) {
	if err := v.Validate(); err != nil {
		return InvocationResult{}, err
	}
	r := InvocationResult{invocationID: v.id, providerCallID: v.call, capability: CapabilityQuestion, wireKind: WireStructured, status: status, code: code, preview: p, questionInput: v.input, questionMetadata: m}
	return initializeResultBudget(r)
}
func (r InvocationResult) QuestionMetadata() QuestionResultMetadata { return r.questionMetadata }
func (r InvocationResult) QuestionAnswers() (QuestionAnswerSet, error) {
	if r.capability != CapabilityQuestion || r.status != ResultSuccess {
		return QuestionAnswerSet{}, fmt.Errorf("question has no committed answers")
	}
	return DecodeQuestionPreview(r.questionInput, []byte(r.preview.text))
}
func (r InvocationResult) validateQuestionResult() error {
	if err := ValidateQuestionResultShape(r.status, r.code, r.preview, r.questionMetadata, r.budget); err != nil {
		return err
	}
	if r.wireKind != WireStructured || r.questionInput.Validate() != nil || r.budget.allowance > MaxQuestionPreviewBytes || r.readMetadata != (ReadResultMetadata{}) || !r.globMetadata.zero() || !r.grepMetadata.zero() || !r.patchMetadata.zero() {
		return fmt.Errorf("invalid question result metadata")
	}
	m := r.questionMetadata
	if m.Fingerprint != r.questionInput.Fingerprint() || m.QuestionCount != len(r.questionInput.Questions()) || m.AnsweredCount < 0 || m.SkippedCount < 0 {
		return fmt.Errorf("invalid question result association")
	}
	if r.status != ResultSuccess {
		if m.AnsweredCount != 0 || m.SkippedCount != 0 {
			return fmt.Errorf("unsuccessful question contains answers")
		}
		return nil
	}
	as, err := r.QuestionAnswers()
	if err != nil {
		return err
	}
	skipped := 0
	for _, a := range as.Answers() {
		if a.Skipped {
			skipped++
		}
	}
	if m.SkippedCount != skipped || m.AnsweredCount != m.QuestionCount-skipped {
		return fmt.Errorf("invalid question answer counts")
	}
	return nil
}

func newQuestionErrorResult(v QuestionInvocation, status ResultStatus, code, message string) InvocationResult {
	p, err := NewModelPreview(message)
	if err != nil {
		return InvocationResult{}
	}
	m := QuestionResultMetadata{Fingerprint: v.input.Fingerprint(), QuestionCount: len(v.input.Questions())}
	r, _ := RestoreQuestionInvocationResult(v, status, code, p, m)
	return r
}
