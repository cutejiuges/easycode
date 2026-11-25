package testsupport

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// StopGoldenHistory 共用两个 Stop 反馈和冻结附件的装配；sample 仍由家族原生结构产生。
func StopGoldenHistory(t *testing.T, c StopConversation, initial domain.InputAssociation, initialNative, sampleNative provider.NativeCommitEnvelope, includeFiles bool, appendSample func(string) provider.NativeCommitEnvelope) (provider.NativeCommitEnvelope, provider.NativeCommitEnvelope, domain.StopFeedback, []provider.RestoreRecord) {
	t.Helper()
	feedback := StopFeedback(t, 0)
	user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000003", "  /review keep indentation\n")
	source, _ := domain.NewSkillSource("project", "review", "easycode")
	attachment, _ := domain.NewSkillAttachment(source, "review", domain.SkillDigest("frozen instructions"), "keep indentation", "frozen instructions")
	user, _ = user.WithSkillAttachment(attachment)
	if includeFiles {
		frozen := FileInput(t, false)
		user, _ = domain.NewUserInput(user.ID(), user.Text()+frozen.Text())
		user, _ = user.WithSkillAttachment(attachment)
		var err error
		user, err = user.WithFileAttachments(frozen.FileAttachments())
		if err != nil {
			t.Fatal(err)
		}
	}

	association, _ := domain.NewInputAssociation(StopTurn, domain.InputSteering, 1, []domain.InputID{user.ID()})
	candidate, _ := domain.NewStopWithSteering(feedback, association, []domain.UserInput{user})
	prepared, err := c.PrepareStopContinuation(candidate)
	if err != nil {
		t.Fatal(err)
	}
	native, err := prepared.FeedbackEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	steer, ok, err := prepared.SteeringEnvelope()
	if err != nil || !ok {
		t.Fatal(err)
	}
	r0, _ := provider.NewInputRestoreRecord(initial, initialNative)
	r1, _ := provider.NewSampleRestoreRecord(StopTurn, sampleNative)
	r2, _ := provider.NewStopFeedbackRestoreRecord(feedback, native)
	r3, _ := provider.NewInputRestoreRecord(association, steer)
	records := []provider.RestoreRecord{r0, r1, r2, r3}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	envelope := appendSample("second answer")
	record, _ := provider.NewSampleRestoreRecord(StopTurn, envelope)
	records = append(records, record)
	feedback2 := StopFeedback(t, 1)
	candidate, _ = domain.NewStopFeedbackOnly(feedback2)
	prepared, err = c.PrepareStopContinuation(candidate)
	if err != nil {
		t.Fatal(err)
	}
	envelope, _ = prepared.FeedbackEnvelope()
	record, _ = provider.NewStopFeedbackRestoreRecord(feedback2, envelope)
	records = append(records, record)
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	return native, steer, feedback2, records
}

// CheckStopGoldens 固定请求、native 与文件摘要的 golden 路径，逐字节比较。
func CheckStopGoldens(t *testing.T, values map[string][]byte, files bool) {
	t.Helper()
	prefix := "testdata/"
	if files {
		prefix += "files_"
	}
	for name, got := range values {
		want, err := os.ReadFile(prefix + name)
		if err != nil || !bytes.Equal(want, got) {
			t.Fatalf("Stop golden differs %s: %v", name, err)
		}
		if files {
			if strings.Contains(name, "checkpoint_request") {
				if bytes.Contains(got, []byte("# Attached workspace file")) {
					t.Fatal("checkpoint rehydrated old files")
				}
			} else if strings.Contains(name, "request.golden") && !bytes.Contains(got, []byte("# Attached workspace file")) {
				t.Fatal("request lost frozen file body", name)
			}
		}
	}
}

// StopCheckpointGolden 保留完整 transcript、revision 和真正请求恢复等价检查。
func StopCheckpointGolden(t *testing.T, c CheckpointConversation, checkpoint *provider.PreparedCompaction, records []provider.RestoreRecord, sampling provider.TurnInput, native, steer provider.NativeCommitEnvelope, before domain.SemanticHistoryView, transcript domain.TranscriptView) ([]byte, []byte) {
	t.Helper()
	envelopeCheckpoint, err := checkpoint.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Finalize(); err != nil {
		t.Fatal(err)
	}
	checkpointRecord, err := provider.NewCheckpointRestoreRecord(envelopeCheckpoint, domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, checkpointRecord)
	recovered, err := c.TestRestore(t, records)
	if err != nil {
		t.Fatal(err)
	}
	_, footprint, err := recovered.ProjectContextWindow()
	if err != nil || footprint.Revision() != 8 {
		t.Fatal("checkpoint counted as native or feedback omitted", err)
	}
	recoveredTranscript, _, err := recovered.ProjectTranscriptFrom(0)
	if err != nil || !reflect.DeepEqual(transcript, recoveredTranscript) || !reflect.DeepEqual(before, recovered.ProjectHistory()) {
		t.Fatal("checkpoint lost full sources", err)
	}
	live := RestoredRequest(t, c, recovered, sampling)
	evidence, _ := codec.MarshalStable(struct {
		NativeRevision  uint64            `json:"native_revision"`
		ContextRevision uint64            `json:"context_revision"`
		Native          []json.RawMessage `json:"native"`
	}{7, footprint.Revision(), []json.RawMessage{native.Payload(), steer.Payload()}})
	return append(live.Bytes(), '\n'), append(evidence, '\n')
}
