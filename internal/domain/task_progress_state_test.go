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

func TestTaskProgressClearReplacementAndContentIdentity(t *testing.T) {
	none := NewNoTaskProgress()
	completed := taskUpdateForTest(t, []TaskStep{taskStepForTest(t, "Inspect", "Inspecting", TaskCompleted)}, "model report")
	a, err := NewTaskProgressState(taskSourceForTest(t, 9), completed)
	if err != nil || a.Kind() != TaskProgressSnapshot || a.Counts().Completed != 1 {
		t.Fatal("completed report lost the saved list", err)
	}
	// 重新规划可回退状态，清除与尚未更新仍是不同的已提交事实。
	replanned := taskUpdateForTest(t, []TaskStep{taskStepForTest(t, "Inspect", "", TaskPending)}, "replanned")
	b, err := NewTaskProgressState(taskSourceForTest(t, 10), replanned)
	if err != nil || b.Counts().Pending != 1 || b.ContentStamp() == a.ContentStamp() {
		t.Fatal("complete replacement did not change content identity", err)
	}
	clear, err := NewTaskProgressState(taskSourceForTest(t, 11), taskUpdateForTest(t, nil, "explicit clear"))
	if err != nil || clear.Kind() != TaskProgressClear || clear.ContentStamp() == none.ContentStamp() || none.ContentStamp() != "" {
		t.Fatal("clear and none became indistinguishable", err)
	}
	update, _ := clear.Update()
	if update.Explanation() != "explicit clear" || string(update.CanonicalJSON()) != `{"items":[],"explanation":"explicit clear"}` {
		t.Fatal("clear lost its frozen explanation")
	}
	otherSource, err := NewTaskProgressSource(30, "018f1d8a-7b5c-7def-8abc-456789abcdea", "018f1d8a-7b5c-7def-8abc-456789abcdeb")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewTaskProgressState(otherSource, completed)
	if err != nil || other.ContentStamp() != a.ContentStamp() {
		t.Fatal("source identity polluted content stamp", err)
	}
	raw := a.CanonicalJSON()
	decoded, err := DecodeTaskProgressState(raw)
	if err != nil || !decoded.Equal(a) {
		t.Fatal("current progress did not preserve the saved fact", err)
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
