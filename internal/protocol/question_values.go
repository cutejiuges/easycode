package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	MaxQuestionInputBytes   = 64 << 10
	MaxQuestionTextBytes    = 16 << 10
	MaxQuestionPreviewBytes = 128 << 10
)

// QuestionID 在一组问题内稳定关联答案，不是 Session 输入身份。
type QuestionID string

func (id QuestionID) Valid() bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for i, c := range string(id) {
		if c < 'a' || c > 'z' {
			if i == 0 || (c != '_' && (c < '0' || c > '9')) {
				return false
			}
		}
	}
	return true
}

// QuestionOption 是模型提供的可见选项，不包含权限。
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question 保存一个有明确选择模式的问题。
type Question struct {
	ID          QuestionID       `json:"id"`
	Header      string           `json:"header"`
	Question    string           `json:"question"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multi_select"`
}

type questionInputWire struct {
	Questions []Question `json:"questions"`
}

// QuestionInput 用不可变编码持有问题，避免共享可变选项。
type QuestionInput struct{ canonical string }

func NewQuestionInput(questions []Question) (QuestionInput, error) {
	if err := validateQuestions(questions); err != nil {
		return QuestionInput{}, err
	}
	b, err := codec.MarshalStable(questionInputWire{questions})
	if err != nil || len(b) > MaxQuestionInputBytes {
		return QuestionInput{}, fmt.Errorf("question input exceeds limit")
	}
	return QuestionInput{string(b)}, nil
}

func DecodeQuestionInput(data []byte) (QuestionInput, error) {
	if len(data) > MaxQuestionInputBytes {
		return QuestionInput{}, fmt.Errorf("question input exceeds limit")
	}
	var w questionInputWire
	if err := codec.UnmarshalRecord(data, &w); err != nil {
		return QuestionInput{}, fmt.Errorf("invalid question input")
	}
	return NewQuestionInput(w.Questions)
}

func (v QuestionInput) Validate() error {
	if v.canonical == "" {
		return fmt.Errorf("question input is required")
	}
	_, err := DecodeQuestionInput([]byte(v.canonical))
	return err
}
func (v QuestionInput) Questions() []Question {
	var w questionInputWire
	_ = codec.Unmarshal([]byte(v.canonical), &w)
	return w.Questions
}
func (v QuestionInput) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return []byte(v.canonical), nil
}
func (v *QuestionInput) UnmarshalJSON(data []byte) error {
	x, err := DecodeQuestionInput(data)
	if err == nil {
		*v = x
	}
	return err
}
func (v QuestionInput) Fingerprint() string {
	if v.canonical == "" {
		return ""
	}
	h := sha256.Sum256([]byte("user.question\n" + v.canonical))
	return hex.EncodeToString(h[:])
}

func validateQuestions(questions []Question) error {
	if len(questions) < 1 || len(questions) > 4 {
		return fmt.Errorf("question count must be between 1 and 4")
	}
	ids := make(map[QuestionID]bool, len(questions))
	texts := make(map[string]bool, len(questions))
	for _, q := range questions {
		if !q.ID.Valid() || ids[q.ID] || texts[q.Question] || !questionTextValid(q.Header, 48) || utf8.RuneCountInString(q.Header) > 12 || !questionTextValid(q.Question, 2048) || len(q.Options) < 2 || len(q.Options) > 4 {
			return fmt.Errorf("invalid question")
		}
		ids[q.ID], texts[q.Question] = true, true
		labels := make(map[string]bool, len(q.Options))
		for _, o := range q.Options {
			if !questionTextValid(o.Label, 128) || !questionTextValid(o.Description, 1024) || labels[o.Label] {
				return fmt.Errorf("invalid question option")
			}
			labels[o.Label] = true
		}
	}
	return nil
}
func questionTextValid(s string, limit int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= limit
}

// QuestionAnswer 保留选项索引、补充原文与显式跳过状态。
type QuestionAnswer struct {
	QuestionID      QuestionID `json:"question_id"`
	SelectedOptions []int      `json:"selected_options"`
	Text            string     `json:"text"`
	Skipped         bool       `json:"skipped"`
}
type questionAnswersWire struct {
	Answers []QuestionAnswer `json:"answers"`
}

// UnmarshalJSON 拒绝显式 null 选项集合，不把它改写为空数组。
func (v *QuestionAnswer) UnmarshalJSON(data []byte) error {
	type wire QuestionAnswer
	var w wire
	if err := codec.DecodeObject(data, &w, "question_id", "selected_options", "text", "skipped"); err != nil {
		return fmt.Errorf("invalid question answer")
	}
	*v = QuestionAnswer(w)
	return nil
}

// QuestionAnswerSet 以不可变编码保存规范化后的有序答案。
type QuestionAnswerSet struct{ canonical string }

func NewQuestionAnswerSet(answers []QuestionAnswer) (QuestionAnswerSet, error) {
	if len(answers) < 1 || len(answers) > 4 {
		return QuestionAnswerSet{}, fmt.Errorf("invalid answer count")
	}
	w := questionAnswersWire{Answers: make([]QuestionAnswer, len(answers))}
	ids := make(map[QuestionID]bool, len(answers))
	total := 0
	for i, a := range answers {
		if !a.QuestionID.Valid() || ids[a.QuestionID] || !utf8.ValidString(a.Text) || len(a.SelectedOptions) > 4 {
			return QuestionAnswerSet{}, fmt.Errorf("invalid question answer")
		}
		ids[a.QuestionID] = true
		total += len(a.Text)
		if total > MaxQuestionTextBytes || (a.Skipped && (len(a.SelectedOptions) != 0 || a.Text != "")) || (!a.Skipped && len(a.SelectedOptions) == 0 && strings.TrimSpace(a.Text) == "") {
			return QuestionAnswerSet{}, fmt.Errorf("invalid question answer")
		}
		a.SelectedOptions = append([]int{}, a.SelectedOptions...)
		sort.Ints(a.SelectedOptions)
		for j, index := range a.SelectedOptions {
			if index < 0 || index > 3 || (j > 0 && index == a.SelectedOptions[j-1]) {
				return QuestionAnswerSet{}, fmt.Errorf("invalid selected option")
			}
		}
		w.Answers[i] = a
	}
	b, err := codec.MarshalStable(w)
	if err != nil || len(b) > MaxQuestionPreviewBytes {
		return QuestionAnswerSet{}, fmt.Errorf("answer encoding exceeds limit")
	}
	return QuestionAnswerSet{string(b)}, nil
}
func DecodeQuestionAnswerSet(data []byte) (QuestionAnswerSet, error) {
	if len(data) > MaxQuestionPreviewBytes {
		return QuestionAnswerSet{}, fmt.Errorf("answer encoding exceeds limit")
	}
	var w questionAnswersWire
	if err := codec.UnmarshalRecord(data, &w); err != nil {
		return QuestionAnswerSet{}, fmt.Errorf("invalid question answers")
	}
	return NewQuestionAnswerSet(w.Answers)
}
func (v QuestionAnswerSet) Validate() error {
	_, err := DecodeQuestionAnswerSet([]byte(v.canonical))
	return err
}
func (v QuestionAnswerSet) ValidateAgainst(input QuestionInput) error {
	if v.Validate() != nil || input.Validate() != nil {
		return fmt.Errorf("invalid question answer association")
	}
	answers, questions := v.Answers(), input.Questions()
	if len(answers) != len(questions) {
		return fmt.Errorf("answer count does not match questions")
	}
	for i, a := range answers {
		q := questions[i]
		if a.QuestionID != q.ID || (!q.MultiSelect && len(a.SelectedOptions) > 1) {
			return fmt.Errorf("answer does not match question")
		}
		for _, index := range a.SelectedOptions {
			if index >= len(q.Options) {
				return fmt.Errorf("answer option is out of range")
			}
		}
	}
	return nil
}
func (v QuestionAnswerSet) Answers() []QuestionAnswer {
	var w questionAnswersWire
	_ = codec.Unmarshal([]byte(v.canonical), &w)
	return w.Answers
}
func (v QuestionAnswerSet) AllSkipped() bool {
	if v.canonical == "" {
		return false
	}
	for _, a := range v.Answers() {
		if !a.Skipped {
			return false
		}
	}
	return true
}
func (v QuestionAnswerSet) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return []byte(v.canonical), nil
}
func (v *QuestionAnswerSet) UnmarshalJSON(data []byte) error {
	x, err := DecodeQuestionAnswerSet(data)
	if err == nil {
		*v = x
	}
	return err
}
