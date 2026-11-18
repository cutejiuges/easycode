package domain

import (
	"fmt"
	"strings"
	"testing"
)

func taskStepForTest(t *testing.T, text, active string, status TaskStatus) TaskStep {
	t.Helper()
	v, err := NewTaskStep(text, active, status)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func taskUpdateForTest(t *testing.T, items []TaskStep, explanation string) TaskUpdate {
	t.Helper()
	v, err := NewTaskUpdate(items, explanation)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTaskUpdateLimitsAndStrictCurrentInput(t *testing.T) {
	label := strings.Repeat("中", 170) + "ab"
	if _, err := NewTaskStep(label, "", TaskPending); err != nil {
		t.Fatal("512-byte UTF-8 label rejected", err)
	}
	for _, invalid := range []string{label + "c", "bad\x1btext", string([]byte{0xff})} {
		if _, err := NewTaskStep(invalid, "", TaskPending); err == nil {
			t.Fatal("invalid task text accepted")
		}
	}
	steps := make([]TaskStep, MaxTaskSteps)
	for i := range steps {
		steps[i] = taskStepForTest(t, fmt.Sprintf("Step %02d", i), "", TaskPending)
	}
	if _, err := NewTaskUpdate(steps, strings.Repeat("x", MaxTaskExplanationBytes)); err != nil {
		t.Fatal("valid complete list rejected", err)
	}
	for _, invalid := range [][]TaskStep{
		append(steps, taskStepForTest(t, "extra", "", TaskPending)),
		{steps[0], steps[0]},
		{taskStepForTest(t, "A", "", TaskInProgress), taskStepForTest(t, "B", "", TaskInProgress)},
	} {
		if _, err := NewTaskUpdate(invalid, ""); err == nil {
			t.Fatal("invalid complete task list accepted")
		}
	}
	if _, err := NewTaskUpdate(nil, strings.Repeat("x", MaxTaskExplanationBytes+1)); err == nil {
		t.Fatal("oversized explanation accepted")
	}
	// 文字总量和转义后的 JSON 是两个独立边界。
	steps = make([]TaskStep, 16)
	for i := range steps {
		steps[i] = taskStepForTest(t, fmt.Sprintf("%02d", i)+strings.Repeat("x", 510), strings.Repeat("y", 512), TaskPending)
	}
	if _, err := NewTaskUpdate(steps, ""); err != nil {
		t.Fatal("16 KiB text limit rejected", err)
	}
	if _, err := NewTaskUpdate(steps, "x"); err == nil {
		t.Fatal("text total overflow accepted")
	}
	for i := range steps {
		steps[i] = taskStepForTest(t, fmt.Sprintf("%02d", i)+strings.Repeat("<", 510), strings.Repeat(">", 512), TaskPending)
	}
	if _, err := NewTaskUpdate(steps, ""); err == nil {
		t.Fatal("escaped JSON overflow accepted")
	}
	valid := `{"items":[{"content":"Inspect","active_form":"","status":"pending"}],"explanation":""}`
	if _, err := DecodeTaskUpdate([]byte(valid)); err != nil {
		t.Fatal("valid current input rejected", err)
	}
	for _, invalid := range []string{
		`{"items":null,"explanation":""}`,
		`{"items":[],"explanation":"","profile":"todo_write"}`,
		strings.Replace(valid, `"status":"pending"`, `"status":"pending","status":"completed"`, 1),
		valid + `{}`,
	} {
		if _, err := DecodeTaskUpdate([]byte(invalid)); err == nil {
			t.Fatal("ambiguous current task input accepted")
		}
	}
}
