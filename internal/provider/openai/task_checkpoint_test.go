package openai

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTaskCheckpointPreservesExactStateAndSource(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("archived work")), nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "archived answer"}}}}}})
	var envelopes []provider.NativeCommitEnvelope
	for _, e := range entries {
		c.history.commit(e)
		v, err := encodeNativeCommit(e)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, v)
	}
	input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
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
	source, err := domain.NewTaskProgressSource(5, entries[0].TurnID, "01900000-0000-7000-8000-000000000002")
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
	preview, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	generation := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", EncryptedContent: "summary-opaque"}, {Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "model summary omits tasks"}}}}}
	prepared, err := c.prepareCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000099", preview.Source(), generation, syntheticRequestReceipt(domain.RequestSummary), state)
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
	summary, err := instance.ProjectCheckpoint(checkpoint)
	if err != nil || summary.Text() != "model summary omits tasks" {
		t.Fatal("task wrapper changed model summary", err)
	}
	// 固定 bytes 必须能独立解码，不能仅验证当前 encoder 自己的输出。
	fixed, err := provider.NewCheckpointEnvelope(checkpoint.Scope(), checkpoint.ID(), checkpoint.Source(), bytes.TrimSpace(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeCheckpoint(fixed); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	wire, err := c.compileSamplingRequest(c.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(wire.Bytes()), "检查中文代码") != 1 || strings.Contains(string(wire.Bytes()), "result_sequence") || strings.Contains(string(wire.Bytes()), "facade_profile") {
		t.Fatal("task prompt is missing, duplicated or contains identity")
	}
	// 即使 payload 与包装重算后自洽，也必须匹配宿主给出的来源前缀事实。
	records := testRestoreRecords(t, envelopes)
	good, err := provider.NewCheckpointRestoreRecord(fixed, state)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, good)
	restored, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	other := restored.(*Conversation)
	restoredWire, err := other.compileSamplingRequest(other.history.snapshot(), input)
	if err != nil || !bytes.Equal(wire.Bytes(), restoredWire.Bytes()) {
		t.Fatal("task checkpoint restore changed canonical request", err)
	}
	a, _ := c.HistoryFootprint()
	b, _ := other.HistoryFootprint()
	if a != b {
		t.Fatal("task checkpoint restore changed footprint")
	}
	wrongSource, _ := domain.NewTaskProgressSource(6, entries[0].TurnID, "01900000-0000-7000-8000-000000000002")
	wrongState, _ := domain.NewTaskProgressState(wrongSource, update)
	for _, expected := range []domain.TaskProgressState{domain.NewNoTaskProgress(), wrongState} {
		bad, err := provider.NewCheckpointRestoreRecord(fixed, expected)
		if err != nil {
			t.Fatal(err)
		}
		changed := append([]provider.RestoreRecord(nil), records...)
		changed[len(changed)-1] = bad
		if _, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", changed); err == nil {
			t.Fatal("checkpoint accepted a mismatched task source")
		}
	}
	forgedPayload := bytes.ReplaceAll(fixed.Payload(), []byte("检查中文代码"), []byte("伪造同长内容"))
	forgedCheckpoint, err := provider.NewCheckpointEnvelope(fixed.Scope(), fixed.ID(), fixed.Source(), forgedPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeCheckpoint(forgedCheckpoint); err != nil {
		t.Fatal("forged fixture was not self-consistent", err)
	}
	forgedRecord, err := provider.NewCheckpointRestoreRecord(forgedCheckpoint, state)
	if err != nil {
		t.Fatal(err)
	}
	forgedRecords := append([]provider.RestoreRecord(nil), records...)
	forgedRecords[len(forgedRecords)-1] = forgedRecord
	if _, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", forgedRecords); err == nil {
		t.Fatal("self-consistent forged task wrapper bypassed expected source facts")
	}
	var current checkpointPayload
	if err := codec.DecodeObject(fixed.Payload(), &current, "summary", "generation", "task_progress"); err != nil {
		t.Fatal(err)
	}
	omitted, err := codec.MarshalStable(struct {
		Summary    NativeItem      `json:"summary"`
		Generation json.RawMessage `json:"generation"`
	}{current.Summary, current.Generation})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{omitted, bytes.Replace(fixed.Payload(), []byte(`"task_progress":`), []byte(`"task_progress":null,"removed":`), 1), bytes.Replace(fixed.Payload(), []byte("检查中文代码"), []byte("伪造同长内容"), 1)} {
		bad, err := provider.NewCheckpointEnvelope(fixed.Scope(), fixed.ID(), fixed.Source(), data)
		if err != nil {
			continue
		}
		if _, _, err := decodeCheckpoint(bad); err == nil {
			t.Fatal("malformed or inconsistent current task checkpoint accepted")
		}
	}
}
