package domain

import "testing"

func taskSourceForTest(t *testing.T, seq uint64) TaskProgressSource {
	t.Helper()
	v, err := NewTaskProgressSource(seq, "018f1d8a-7b5c-7def-8abc-456789abcdef", "018f1d8a-7b5c-7def-8abc-456789abcdee")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTaskProgressContentIdentityAndStrictState(t *testing.T) {
	none := NewNoTaskProgress()
	completed := taskUpdateForTest(t, []TaskStep{taskStepForTest(t, "Inspect", "Inspecting", TaskCompleted)}, "model report")
	a, err := NewTaskProgressState(taskSourceForTest(t, 9), completed)
	if err != nil {
		t.Fatal(err)
	}
	// 重新规划可回退状态，清除与尚未更新仍是不同的已提交事实。
	replanned := taskUpdateForTest(t, []TaskStep{taskStepForTest(t, "Inspect", "", TaskPending)}, "replanned")
	b, err := NewTaskProgressState(taskSourceForTest(t, 10), replanned)
	if err != nil || b.ContentStamp() == a.ContentStamp() {
		t.Fatal("complete replacement did not change content identity", err)
	}
	clear, err := NewTaskProgressState(taskSourceForTest(t, 11), taskUpdateForTest(t, nil, "explicit clear"))
	if err != nil || clear.ContentStamp() == none.ContentStamp() || none.ContentStamp() != "" {
		t.Fatal("clear and none became indistinguishable", err)
	}
	otherSource, err := NewTaskProgressSource(30, "018f1d8a-7b5c-7def-8abc-456789abcdea", "018f1d8a-7b5c-7def-8abc-456789abcdeb")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewTaskProgressState(otherSource, completed)
	if err != nil || other.ContentStamp() != a.ContentStamp() {
		t.Fatal("source identity polluted content stamp", err)
	}
	for _, invalid := range []string{
		`{"state":"none","source":null}`,
		`{"state":"none","profile":"todo_write"}`,
		`{"state":"snapshot","update":{"items":[],"explanation":""}}`,
	} {
		if _, err := DecodeTaskProgressState([]byte(invalid)); err == nil {
			t.Fatal("invalid current progress accepted")
		}
	}
	if !IsReservedSkillSlashName("tasks") || IsReservedSkillSlashName("tasks-extra") {
		t.Fatal("task command displaced the skill naming boundary")
	}
}
