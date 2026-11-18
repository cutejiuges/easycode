package headless

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestTasksQueryStrictFragmentationAndDedicatedLimit(t *testing.T) {
	valid := `{"version":1,"type":"tasks.query","request_id":"任务-query"}`
	reader, err := NewStreamCommandReader(&fragmentedReader{data: []byte(valid), sizes: []int{1, 3, 2, 7}})
	if err != nil {
		t.Fatal(err)
	}
	command, err := reader.ReadCommand()
	if err != nil || command.Kind() != protocol.CommandQueryTasks {
		t.Fatal("fragmented EOF query rejected", err)
	}
	if _, err := reader.ReadCommand(); err != io.EOF {
		t.Fatal("query EOF was not drained", err)
	}
	base := `{"version":1,"type":"tasks.query","request_id":"q"`
	if _, err := DecodeStreamCommand([]byte(base + `,"text":"private-secret"}`)); err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("unsafe extra query payload", err)
	}

	for _, limit := range []int{MaxTaskQueryBytes, MaxTaskQueryBytes + 1} {
		raw := []byte(base + "}" + strings.Repeat(" ", limit-len(base)-1))
		_, err := DecodeStreamCommand(raw)
		if (err != nil) != (limit > MaxTaskQueryBytes) {
			t.Fatalf("dedicated limit %d: %v", limit, err)
		}
	}
	// 显式提交的 slash 文本仍是用户输入，不转为宿主查询。
	for _, kind := range []string{"input.submit", "input.append"} {
		c, err := DecodeStreamCommand([]byte(`{"version":1,"type":"` + kind + `","request_id":"input","text":"/tasks"}`))
		if err != nil || c.Text() != "/tasks" || c.Kind() == protocol.CommandQueryTasks {
			t.Fatal("literal input was intercepted", err)
		}
	}
}

func headlessTaskState(t *testing.T, clear bool) domain.TaskProgressState {
	t.Helper()
	var steps []domain.TaskStep
	if !clear {
		step, _ := domain.NewTaskStep("检查任务 🧑‍💻", "正在检查任务", domain.TaskInProgress)
		steps = []domain.TaskStep{step}
	}
	update, err := domain.NewTaskUpdate(steps, "保留完整说明")
	if err != nil {
		t.Fatal(err)
	}
	source, err := domain.NewTaskProgressSource(11, "01900000-0000-7000-8000-000000000003", "01900000-0000-7000-8000-000000000004")
	if err != nil {
		t.Fatal(err)
	}
	state, err := domain.NewTaskProgressState(source, update)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTaskQueryResponseThenSnapshotAndStrictEnvelope(t *testing.T) {
	sid := domain.SessionID("01900000-0000-7000-8000-000000000001")
	tid := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	for _, state := range []domain.TaskProgressState{headlessTaskState(t, false)} {
		projector, err := NewStreamProjector(sid, tid)
		if err != nil {
			t.Fatal(err)
		}
		cmd, _ := protocol.NewQueryTasksCommand("query")
		result, _ := protocol.NewAcceptedCommandResult(cmd, protocol.CommandDispositionQueried)
		response, _ := protocol.NewCommandResultItem(result)
		snapshot, _ := protocol.NewTaskProgressSnapshot(sid, tid, state)
		item, _ := protocol.NewCorrelatedContextItem("query", snapshot)
		var output bytes.Buffer
		for _, v := range []protocol.ControlItem{response, item} {
			event, err := projector.Project(v)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewStreamEncoder(&output).Write(event); err != nil {
				t.Fatal(err)
			}
		}
		lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
		if len(lines) != 2 || !bytes.Contains(lines[0], []byte(`"command_type":"tasks.query"`)) || !bytes.Contains(lines[0], []byte(`"disposition":"queried"`)) {
			t.Fatal("query response ordering", output.String())
		}
		if state.Kind() == domain.TaskProgressSnapshot {
			golden, err := os.ReadFile("testdata/tasks.golden.jsonl")
			if err != nil || !bytes.Equal(golden, append(append([]byte(nil), lines[1]...), '\n')) {
				t.Fatalf("task snapshot golden changed: %v; got=%s", err, lines[1])
			}
		}
		decoded, err := DecodeTaskSnapshotEvent(lines[1])
		if err != nil || !decoded.Snapshot.Progress.Equal(state) || decoded.RequestID != "query" || decoded.SessionID != sid || decoded.ThreadID != tid {
			t.Fatal("snapshot scope mismatch", err)
		}
		for _, forbidden := range []string{`"profile"`, `"input_id"`, `"compaction_id"`, `"turn_id":null`} {
			if bytes.Contains(lines[1], []byte(forbidden)) {
				t.Fatal("snapshot leaked unrelated identity", output.String())
			}
		}
		// 外层和内层都严格拒绝未知字段、null、计数伪造和重复键。
		for _, raw := range [][]byte{
			bytes.Replace(lines[1], []byte(`"snapshot":`), []byte(`"profile":"todo_write","snapshot":`), 1),
			bytes.Replace(lines[1], []byte(`"total":`), []byte(`"total":999,"extra":`), 1),
		} {
			if _, err := DecodeTaskSnapshotEvent(raw); err == nil {
				t.Fatal("invalid snapshot accepted", string(raw))
			}
		}
	}
}

func TestTaskUpdatedIsValidatedAndFilteredInOneShot(t *testing.T) {
	sid := domain.SessionID("01900000-0000-7000-8000-000000000001")
	tid := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000003")
	p := newProjector(sid, tid)
	started := protocol.NewTurnStarted()
	started.SessionID, started.ThreadID, started.TurnID = sid, tid, turn
	if _, err := p.project(started); err != nil {
		t.Fatal(err)
	}
	updated, err := protocol.NewTaskProgressUpdated(sid, tid, turn, headlessTaskState(t, false))
	if err != nil {
		t.Fatal(err)
	}
	projected, err := p.project(updated)
	if err != nil || projected.event != nil || projected.text != "" {
		t.Fatal("unsolicited task frame", err)
	}
	broken := updated
	broken.Payload = []byte(`{"progress":null,"counts":{}}`)
	if _, err := p.project(broken); err == nil {
		t.Fatal("invalid internal projection silently ignored")
	}
}

func TestTaskSnapshotMaximumCanonicalAndEncodedCap(t *testing.T) {
	state := headlessTaskState(t, false)
	value := StreamTaskSnapshotEvent{Version: 1, Type: "tasks.snapshot", RequestID: "q", SessionID: "01900000-0000-7000-8000-000000000001", ThreadID: "01900000-0000-7000-8000-000000000002", Snapshot: protocol.TaskProgressSnapshotPayload{Progress: state, Counts: state.Counts()}}
	raw, err := codec.MarshalStable(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTaskSnapshotEvent(raw); err != nil {
		t.Fatal("maximum legal snapshot rejected", len(raw), err)
	}
	if _, err := DecodeTaskSnapshotEvent(append(raw, bytes.Repeat([]byte{' '}, MaxTaskSnapshotBytes-len(raw))...)); err == nil {
		t.Fatal("encoded snapshot cap ignored")
	}
}

// taskQueryStreamLoop 复用既有 owner 生命周期，只为查询提供真实两帧输出。
type taskQueryStreamLoop struct {
	*scriptedStreamLoop
	state domain.TaskProgressState
}

func (loop *taskQueryStreamLoop) Submit(ctx context.Context, command protocol.Command) (protocol.CommandResult, error) {
	if command.Kind() != protocol.CommandQueryTasks {
		return loop.scriptedStreamLoop.Submit(ctx, command)
	}
	result, err := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	response, _ := protocol.NewCommandResultItem(result)
	event, err := protocol.NewTaskProgressSnapshot(testSessionID, testThreadID, loop.state)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	snapshot, err := protocol.NewCorrelatedTaskProgressItem(command.RequestID(), event)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	loop.mu.Lock()
	defer loop.mu.Unlock()
	loop.outputs <- response
	loop.outputs <- snapshot
	return result, nil
}

func TestTaskQueryEOFDrainAndSnapshotWriteFailureOwnCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "drain"
		if fail {
			name = "snapshot_broken_pipe"
		}
		t.Run(name, func(t *testing.T) {
			state := headlessTaskState(t, false)
			loop := &taskQueryStreamLoop{newScriptedStreamLoop(false), state}
			wire := `{"version":1,"type":"tasks.query","request_id":"task-query"}` + "\n"
			output := &failAfterWriter{allowed: 10, closed: make(chan struct{})}
			if fail {
				output.allowed = 2
			}
			runner := newTestStreamRunner(t, loop, io.NopCloser(strings.NewReader(wire)), output, output)
			result := runner.Run(context.Background())
			<-loop.Done()
			if fail {
				if !result.OutputUnavailable || output.writes != 3 || strings.Contains(output.String(), "tasks.snapshot") {
					t.Fatal("snapshot write failed without terminal owner cleanup", result, output.String())
				}
			} else if !result.Completed || output.writes != 3 || strings.Count(output.String(), `"type":"tasks.snapshot"`) != 1 {
				t.Fatal("EOF did not drain correlated snapshot", result, output.String())
			}
			if !loop.state.Equal(state) {
				t.Fatal("transport failure rolled back task state")
			}
		})
	}
}
