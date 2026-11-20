package protocol

import (
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func taskStateForTest(t *testing.T) domain.TaskProgressState {
	t.Helper()
	step, err := domain.NewTaskStep("Inspect", "Inspecting", domain.TaskInProgress)
	if err != nil {
		t.Fatal(err)
	}
	update, err := domain.NewTaskUpdate([]domain.TaskStep{step}, "Review\nresult")
	if err != nil {
		t.Fatal(err)
	}
	source, err := domain.NewTaskProgressSource(9, domain.TurnID("018f1d8a-7b5c-7def-8abc-456789abcdef"), "018f1d8a-7b5c-7def-8abc-456789abcdee")
	if err != nil {
		t.Fatal(err)
	}
	state, err := domain.NewTaskProgressState(source, update)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func taskScopeForTest() (domain.SessionID, domain.ThreadID, domain.TurnID) {
	return "018f1d8a-7b5c-7def-8abc-456789abcdec", "018f1d8a-7b5c-7def-8abc-456789abcded", "018f1d8a-7b5c-7def-8abc-456789abcdef"
}

func TestTaskQueryCommandAndCorrelatedSnapshot(t *testing.T) {
	command, err := NewQueryTasksCommand("query-tasks")
	if err != nil || command.Kind() != CommandQueryTasks || command.Text() != "" || command.ExpectedTurnID() != "" {
		t.Fatal("invalid query command", err)
	}
	result, err := NewAcceptedCommandResult(command, CommandDispositionQueried)
	if err != nil || result.Validate() != nil {
		t.Fatal("query admission invalid", err)
	}
	if _, err := NewAcceptedCommandResult(command, CommandDispositionUpdated); err == nil {
		t.Fatal("query accepted as an update")
	}
	if _, err := NewQueryTasksCommand(""); err == nil {
		t.Fatal("query without request identity accepted")
	}
	for _, forged := range []Command{
		{kind: CommandQueryTasks, requestID: "q", text: "extra"},
		{kind: CommandQueryTasks, requestID: "q", instructions: "extra"},
		{kind: CommandQueryTasks, requestID: "q", expectedTurnID: "018f1d8a-7b5c-7def-8abc-456789abcdef"},
	} {
		if forged.Validate() == nil {
			t.Fatal("query accepted unrelated payload")
		}
	}
	sessionID, threadID, _ := taskScopeForTest()
	event, err := NewTaskProgressSnapshot(sessionID, threadID, domain.NewNoTaskProgress())
	if err != nil || event.TurnID != "" || event.Validate() != nil {
		t.Fatal("none query has invalid scope", err)
	}
	const golden = `{"progress":{"state":"none"},"counts":{"total":0,"pending":0,"in_progress":0,"completed":0}}`
	if string(event.Payload) != golden {
		t.Fatalf("none snapshot golden changed: %s", event.Payload)
	}
	item, err := NewCorrelatedTaskProgressItem(command.RequestID(), event)
	if err != nil || item.Validate() != nil {
		t.Fatal("correlated task snapshot invalid", err)
	}
	copyEvent, ok := item.ContextEvent()
	if !ok || copyEvent.Kind != EventTaskProgressSnapshot {
		t.Fatal("query was not a correlated context item")
	}
	copyEvent.Payload[0] = '['
	if original, _ := item.ContextEvent(); original.Payload[0] != '{' {
		t.Fatal("query item shares mutable event payload")
	}
}

func TestTaskProgressEventsBindCommittedSource(t *testing.T) {
	sessionID, threadID, turnID := taskScopeForTest()
	state := taskStateForTest(t)
	query, err := NewTaskProgressSnapshot(sessionID, threadID, state)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := DecodeTaskProgressSnapshot(query)
	if err != nil || !payload.Progress.Equal(state) || payload.Counts != (domain.TaskCounts{Total: 1, InProgress: 1}) || !payload.Equal(payload.Clone()) {
		t.Fatal("query lost complete progress", err)
	}
	event, err := NewTaskProgressUpdated(sessionID, threadID, turnID, state)
	if err != nil || event.Validate() != nil {
		t.Fatal("committed update rejected", err)
	}
	if _, err := NewCorrelatedTurnItem("request", event); err != nil {
		t.Fatal("update cannot be delivered by the turn owner", err)
	}
	if _, err := NewCorrelatedTaskProgressItem("query", event); err == nil {
		t.Fatal("unsolicited update used the query channel")
	}
	if _, err := NewTaskProgressUpdated(sessionID, threadID, turnID, domain.NewNoTaskProgress()); err == nil {
		t.Fatal("update fabricated a none result")
	}
	if _, err := NewTaskProgressUpdated(sessionID, threadID, "018f1d8a-7b5c-7def-8abc-456789abcdea", state); err == nil {
		t.Fatal("update source turn mismatch accepted")
	}
	forged := query
	forged.TurnID = turnID
	if forged.Validate() == nil {
		t.Fatal("query acquired a fake active turn")
	}
	if !ToolTaskProgress.Valid() || ToolName("TodoWrite").Valid() || ToolName("update_plan").Valid() {
		t.Fatal("product tool identity leaked facade names")
	}
}

func TestTaskSnapshotRejectsMissingConflictingAndOversizedFields(t *testing.T) {
	sessionID, threadID, _ := taskScopeForTest()
	event, err := NewTaskProgressSnapshot(sessionID, threadID, taskStateForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	valid := string(event.Payload)
	for _, raw := range []string{
		`{}`, `{"progress":{"state":"none"},"counts":null}`,
		`{"progress":{"state":"none"},"counts":{}}`,
		strings.Replace(valid, `"total":1`, `"total":2`, 1),
		strings.Replace(valid, `"in_progress":1`, `"in_progress":0`, 1),
		strings.Replace(valid, `"completed":0`, `"completed":0,"completed":1`, 1),
		strings.Replace(valid, `"explanation":`, `"profile":"todo_write","explanation":`, 1),
		strings.Replace(valid, `"result_sequence":9`, `"result_sequence":0`, 1),
		valid + `{}`, strings.Repeat(" ", MaxTaskSnapshotPayloadBytes) + valid,
	} {
		forged := event
		forged.Payload = []byte(raw)
		if _, err := DecodeTaskProgressSnapshot(forged); err == nil || forged.Validate() == nil {
			t.Fatalf("invalid query accepted with %d payload bytes", len(raw))
		}
	}
	for _, mutate := range []func(*Event){
		func(e *Event) { e.SessionID = "" },
		func(e *Event) { e.ThreadID = "" },
		func(e *Event) { e.CompactionID = "018f1d8a-7b5c-7def-8abc-456789abcdef" },
		func(e *Event) { e.Timestamp = time.Time{} },
		func(e *Event) { e.ItemID = "unexpected" },
	} {
		forged := event
		mutate(&forged)
		if _, err := DecodeTaskProgressSnapshot(forged); err == nil {
			t.Fatal("invalid full event identity accepted")
		}
	}
	encoded, err := codec.MarshalStable(event)
	if err != nil || len(encoded) > MaxTaskSnapshotPayloadBytes {
		t.Fatal("complete snapshot was not bounded", err)
	}
	decoded, _ := DecodeTaskProgressSnapshot(event)
	decoded.Counts.Completed++
	if decoded.Validate() == nil {
		t.Fatal("forged derived count accepted")
	}
}
