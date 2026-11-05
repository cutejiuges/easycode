package protocol

import (
	"strings"
	"testing"

	"easycode/internal/codec"
)

func questionFixture() []Question {
	return []Question{{ID: "storage", Header: "存储", Question: "请选择存储方式？", Options: []QuestionOption{{Label: "Local", Description: "本地"}, {Label: "Remote", Description: "远程"}}, MultiSelect: false}}
}
func TestQuestionInputStrictAndImmutable(t *testing.T) {
	questions := questionFixture()
	input, err := NewQuestionInput(questions)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := input.MarshalJSON()
	fingerprint := input.Fingerprint()
	questions[0].Options[0].Label = "changed"
	got := input.Questions()
	got[0].Options[0].Description = "changed"
	after, _ := input.MarshalJSON()
	if string(before) != string(after) || input.Fingerprint() != fingerprint {
		t.Fatal("mutable question snapshot")
	}
	decoded, err := DecodeQuestionInput(before)
	if err != nil || decoded != input {
		t.Fatalf("roundtrip: %v", err)
	}
	valid := string(before)
	for name, data := range map[string]string{
		"unknown":   strings.Replace(valid, `"questions":`, `"answers":[],"questions":`, 1),
		"duplicate": strings.Replace(valid, `"id":"storage"`, `"id":"storage","id":"other"`, 1),
		"alias":     strings.Replace(valid, `"header":`, `"Header":`, 1),
		"null":      strings.Replace(valid, `"multi_select":false`, `"multi_select":null`, 1),
		"missing":   strings.Replace(valid, `,"multi_select":false`, "", 1),
		"trailing":  valid + ` {}`,
		"surrogate": strings.Replace(valid, `"storage"`, `"\ud800"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeQuestionInput([]byte(data)); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}
func TestQuestionInputBounds(t *testing.T) {
	for _, n := range []int{0, 5} {
		if _, err := NewQuestionInput(make([]Question, n)); err == nil {
			t.Fatal("accepted invalid count")
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Question)
		valid  bool
	}{
		{"question exact", func(q *Question) { q.Question = strings.Repeat("q", 2048) }, true},
		{"question overflow", func(q *Question) { q.Question = strings.Repeat("q", 2049) }, false},
		{"header exact", func(q *Question) { q.Header = strings.Repeat("中", 12) }, true},
		{"header overflow", func(q *Question) { q.Header = strings.Repeat("中", 13) }, false},
		{"id exact", func(q *Question) { q.ID = QuestionID(strings.Repeat("a", 64)) }, true},
		{"id overflow", func(q *Question) { q.ID = QuestionID(strings.Repeat("a", 65)) }, false},
		{"id invalid", func(q *Question) { q.ID = "A" }, false},
		{"label exact", func(q *Question) { q.Options[0].Label = strings.Repeat("a", 128) }, true},
		{"label overflow", func(q *Question) { q.Options[0].Label = strings.Repeat("a", 129) }, false},
		{"description exact", func(q *Question) { q.Options[0].Description = strings.Repeat("a", 1024) }, true},
		{"description overflow", func(q *Question) { q.Options[0].Description = strings.Repeat("a", 1025) }, false},
		{"duplicate label", func(q *Question) { q.Options[1] = q.Options[0] }, false},
		{"invalid unicode", func(q *Question) { q.Header = string([]byte{255}) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			qs := questionFixture()
			tc.mutate(&qs[0])
			_, err := NewQuestionInput(qs)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
	q := questionFixture()[0]
	if _, err := NewQuestionInput([]Question{q, q}); err == nil {
		t.Fatal("accepted duplicate question")
	}
	input, _ := NewQuestionInput(questionFixture())
	b, _ := input.MarshalJSON()
	if _, err := DecodeQuestionInput(append(b, []byte(strings.Repeat(" ", MaxQuestionInputBytes))...)); err == nil {
		t.Fatal("accepted oversized wire")
	}
}
func TestQuestionAnswerAssociationAndBounds(t *testing.T) {
	qs := questionFixture()
	qs[0].MultiSelect = true
	input, _ := NewQuestionInput(qs)
	a, err := NewQuestionAnswerSet([]QuestionAnswer{{QuestionID: "storage", SelectedOptions: []int{1, 0}, Text: " 说明\n"}})
	if err != nil || a.ValidateAgainst(input) != nil {
		t.Fatal(err)
	}
	answer := a.Answers()[0]
	if answer.SelectedOptions[0] != 0 || answer.Text != " 说明\n" {
		t.Fatal("lost answer semantics")
	}
	answer.SelectedOptions[0] = 3
	if a.Answers()[0].SelectedOptions[0] != 0 {
		t.Fatal("shared answer slice")
	}
	single, _ := NewQuestionInput(questionFixture())
	if a.ValidateAgainst(single) == nil {
		t.Fatal("accepted multiple single choices")
	}
	for _, answer := range []QuestionAnswer{
		{QuestionID: "storage", SelectedOptions: []int{0, 0}},
		{QuestionID: "storage", SelectedOptions: []int{-1}},
		{QuestionID: "storage", SelectedOptions: []int{4}},
		{QuestionID: "storage", Text: " "},
		{QuestionID: "storage", Skipped: true, Text: "answer"},
		{QuestionID: "storage", Text: strings.Repeat("a", MaxQuestionTextBytes+1)},
	} {
		if _, err := NewQuestionAnswerSet([]QuestionAnswer{answer}); err == nil {
			t.Fatal("accepted invalid answer")
		}
	}
	exact, err := NewQuestionAnswerSet([]QuestionAnswer{{QuestionID: "storage", Text: strings.Repeat("a", MaxQuestionTextBytes)}})
	if err != nil || exact.ValidateAgainst(single) != nil {
		t.Fatal(err)
	}
	skipped, err := NewQuestionAnswerSet([]QuestionAnswer{{QuestionID: "storage", Skipped: true}})
	if err != nil || !skipped.AllSkipped() || skipped.ValidateAgainst(input) != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`{"answers":[{"question_id":"storage","selected_options":null,"text":"x","skipped":false}]}`, `{"answers":[{"question_id":"storage","selected_options":[],"text":"x","skipped":false,"extra":1}]}`} {
		if _, err := DecodeQuestionAnswerSet([]byte(s)); err == nil {
			t.Fatal("accepted invalid answer wire")
		}
	}
	b, _ := codec.MarshalStable(skipped)
	restored, err := DecodeQuestionAnswerSet(b)
	if err != nil || restored != skipped {
		t.Fatal("answer roundtrip", err)
	}
}
