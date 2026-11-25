package testsupport

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// CheckTaskCheckpoint 对两家的固定 checkpoint 验证任务原文、来源及完整恢复，不生成 fixture。
func CheckTaskCheckpoint(t *testing.T, c CheckpointConversation, input provider.TurnInput, turn domain.TurnID, records []provider.RestoreRecord, decode func(provider.CheckpointEnvelope) (domain.ContextSummary, error)) {
	t.Helper()
	compact, err := provider.NewCompactionInput(input, "preserve tasks", 4096)
	if err != nil {
		t.Fatal(err)
	}
	step, err := domain.NewTaskStep("检查中文代码", "正在检查", domain.TaskInProgress)
	if err != nil {
		t.Fatal(err)
	}
	update, err := domain.NewTaskUpdate([]domain.TaskStep{step}, "保留完整进度")
	if err != nil {
		t.Fatal(err)
	}
	source, err := domain.NewTaskProgressSource(5, turn, "01900000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	state, err := domain.NewTaskProgressState(source, update)
	if err != nil {
		t.Fatal(err)
	}
	compact, err = compact.WithTaskProgress(state)
	if err != nil || !compact.TaskProgress().Equal(state) {
		t.Fatal("compaction state missing", err)
	}
	preview, _, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := c.TestCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000099", preview.Source(), "model summary omits tasks", "summary-opaque", state)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/task_checkpoint.current.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(fixture), checkpoint.Payload()) {
		t.Fatalf("task checkpoint fixture differs: %s", checkpoint.Payload())
	}
	decoded, err := decode(checkpoint)
	if err != nil || decoded.Text() != "model summary omits tasks" {
		t.Fatal("task wrapper changed model summary", err)
	}
	// 固定 bytes 必须能独立解码，不能仅验证当前 encoder 自己的输出。
	fixed, err := provider.NewCheckpointEnvelope(checkpoint.Scope(), checkpoint.ID(), checkpoint.Source(), bytes.TrimSpace(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(fixed); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	wire, err := c.TestRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(wire.Bytes()), "检查中文代码") != 1 || strings.Contains(string(wire.Bytes()), "result_sequence") || strings.Contains(string(wire.Bytes()), "facade_profile") {
		t.Fatal("task prompt is missing, duplicated or contains identity")
	}
	// 即使 payload 与包装重算后自洽，也必须匹配宿主给出的来源前缀事实。
	good, err := provider.NewCheckpointRestoreRecord(fixed, state)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, good)
	restored, err := c.TestRestore(t, records)
	if err != nil {
		t.Fatal(err)
	}
	RestoredRequest(t, c, restored, input)
	wrongSource, _ := domain.NewTaskProgressSource(6, turn, "01900000-0000-7000-8000-000000000002")
	wrongState, _ := domain.NewTaskProgressState(wrongSource, update)
	for _, expected := range []domain.TaskProgressState{domain.NewNoTaskProgress(), wrongState} {
		bad, err := provider.NewCheckpointRestoreRecord(fixed, expected)
		if err != nil {
			t.Fatal(err)
		}
		changed := append([]provider.RestoreRecord(nil), records...)
		changed[len(changed)-1] = bad
		if _, err := c.TestRestore(t, changed); err == nil {
			t.Fatal("checkpoint accepted a mismatched task source")
		}
	}
	forgedPayload := bytes.ReplaceAll(fixed.Payload(), []byte("检查中文代码"), []byte("伪造同长内容"))
	forgedCheckpoint, err := provider.NewCheckpointEnvelope(fixed.Scope(), fixed.ID(), fixed.Source(), forgedPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(forgedCheckpoint); err != nil {
		t.Fatal("forged fixture was not self-consistent", err)
	}
	forgedRecord, err := provider.NewCheckpointRestoreRecord(forgedCheckpoint, state)
	if err != nil {
		t.Fatal(err)
	}
	forgedRecords := append([]provider.RestoreRecord(nil), records...)
	forgedRecords[len(forgedRecords)-1] = forgedRecord
	if _, err := c.TestRestore(t, forgedRecords); err == nil {
		t.Fatal("self-consistent forged task wrapper bypassed expected source facts")
	}
	current, err := codec.ObjectMembers(fixed.Payload())
	if err != nil {
		t.Fatal(err)
	}
	delete(current, "task_progress")
	omitted, err := codec.MarshalStable(current)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{omitted, bytes.Replace(fixed.Payload(), []byte(`"task_progress":`), []byte(`"task_progress":null,"removed":`), 1), bytes.Replace(fixed.Payload(), []byte("检查中文代码"), []byte("伪造同长内容"), 1)} {
		bad, err := provider.NewCheckpointEnvelope(fixed.Scope(), fixed.ID(), fixed.Source(), data)
		if err != nil {
			continue
		}
		if _, err := decode(bad); err == nil {
			t.Fatal("malformed or inconsistent current task checkpoint accepted")
		}
	}
}
