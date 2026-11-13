package anthropic

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// stopGoldenValues 固定两次反馈、真实 Skill 纠正、opaque 和完整窗口摘要的请求证据。
func stopGoldenValues(t *testing.T) map[string][]byte {
	t.Helper()
	c, initial, initialEntry := stopTestConversation(t)
	feedback := stopTestFeedback(t, 0)
	user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000003", "  /review keep indentation\n")
	source, _ := domain.NewSkillSource("project", "review", "easycode")
	attachment, _ := domain.NewSkillAttachment(source, "review", domain.SkillDigest("frozen instructions"), "keep indentation", "frozen instructions")
	user, _ = user.WithSkillAttachment(attachment)
	association, _ := domain.NewInputAssociation(stopTestTurn, domain.InputSteering, 1, []domain.InputID{user.ID()})
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
	initialNative, _ := encodeNativeCommit(initialEntry)
	sampleNative, _ := encodeNativeCommit(c.history.snapshot()[1])
	r0, _ := provider.NewInputRestoreRecord(initial, initialNative)
	r1, _ := provider.NewSampleRestoreRecord(stopTestTurn, sampleNative)
	r2, _ := provider.NewStopFeedbackRestoreRecord(feedback, native)
	r3, _ := provider.NewInputRestoreRecord(association, steer)
	records := []provider.RestoreRecord{r0, r1, r2, r3}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	sample := stopTestSample(stopTestTurn, "second answer")
	c.history.commit(sample)
	envelope, _ := encodeNativeCommit(sample)
	record, _ := provider.NewSampleRestoreRecord(stopTestTurn, envelope)
	records = append(records, record)
	feedback2 := stopTestFeedback(t, 1)
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
	restored := testConversation(t, c.provider)
	if err := restoreNativeHistory(context.Background(), "model", records, &restored.history); err != nil {
		t.Fatal(err)
	}
	sampling, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string][]byte{"stop_feedback_native.golden.json": append(native.Payload(), '\n')}
	for _, policy := range []domain.CachePolicy{domain.DefaultCachePolicy(), mustStopCachePolicy(t, domain.AnthropicCache5m), mustStopCachePolicy(t, domain.AnthropicCache1h)} {
		c.provider.config.CachePolicy = policy
		request, err := c.compileSamplingRequest(c.history.snapshot(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		again, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
		if err != nil || !bytes.Equal(request.Bytes(), again.Bytes()) {
			t.Fatal("restored request differs", err)
		}
		original, err := c.HistoryFootprint()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := restored.HistoryFootprint()
		if err != nil || original != actual || original.Revision() != 6 {
			t.Fatal("footprint/revision changed", err)
		}
		if !reflect.DeepEqual(c.ProjectHistory(), restored.ProjectHistory()) {
			t.Fatal("semantic source differs")
		}
		body := request.Bytes()
		if !bytes.Contains(body, []byte("PRIVATE-OPAQUE")) || !bytes.Contains(body, []byte("frozen instructions")) || bytes.Count(body, []byte("completion_check_feedback")) != 4 {
			t.Fatal("source lost or duplicated")
		}
		name := string(policy.Anthropic())
		values["stop_"+name+"_request.golden.json"] = append(body, '\n')

	}
	// 后续摘要覆盖完整旧窗口；已消费的反馈进入摘要来源，完整 transcript 永不丢失。
	sample = stopTestSample(stopTestTurn, "final answer")
	c.history.commit(sample)
	envelope, _ = encodeNativeCommit(sample)
	record, _ = provider.NewSampleRestoreRecord(stopTestTurn, envelope)
	records = append(records, record)
	snapshot := c.history.snapshotWindow()
	sourceBefore, err := nativeCompactionSource(snapshot, "model")
	if err != nil {
		t.Fatal(err)
	}
	changedReason, _ := domain.NewHookFeedbackCandidate("different authorized reason")
	changedFeedback, err := domain.NewStopFeedback(domain.HookRunResult{Identity: feedback2.Identity, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, feedback2.Reference, changedReason)
	if err != nil {
		t.Fatal(err)
	}
	changedEntry, err := stopFeedbackEntry(changedFeedback)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.entries[5] = changedEntry
	sourceAfter, err := nativeCompactionSource(snapshot, "model")
	if err != nil || sourceBefore.Digest() == sourceAfter.Digest() {
		t.Fatal("compaction digest omitted delivered feedback", err)
	}
	before := c.ProjectHistory()
	transcript, err := c.ProjectTranscript()
	if err != nil {
		t.Fatal(err)
	}
	compact, err := provider.NewCompactionInput(sampling, "preserve completed checks", 4096)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	request, err := c.compileCompactionRequest(compact)
	if err != nil {
		t.Fatal(err)
	}
	values["stop_compaction_request.golden.json"] = append(request.Bytes(), '\n')
	generated := stopTestSample(stopTestTurn, "goal; completed tests; next step")
	generated.Metadata.StopReason = optionalString{Known: true, Value: "end_turn"}
	checkpoint, err := c.prepareCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000004", preview.Source(), generated, syntheticRequestReceipt(domain.RequestSummary))
	if err != nil {
		t.Fatal(err)
	}
	envelopeCheckpoint, err := checkpoint.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Finalize(); err != nil {
		t.Fatal(err)
	}
	checkpointRecord, err := provider.NewCheckpointRestoreRecord(envelopeCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, checkpointRecord)
	value, err := c.provider.RestoreConversation(c.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	recovered := value.(*Conversation)
	footprint, err := recovered.HistoryFootprint()
	if err != nil || footprint.Revision() != 8 {
		t.Fatal("checkpoint counted as native or feedback omitted", err)
	}
	recoveredTranscript, err := recovered.ProjectTranscript()
	if err != nil || !reflect.DeepEqual(transcript, recoveredTranscript) || !reflect.DeepEqual(before, recovered.ProjectHistory()) {
		t.Fatal("checkpoint lost full sources", err)
	}
	live, err := c.compileSamplingRequest(c.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := recovered.compileSamplingRequest(recovered.history.snapshot(), sampling)
	if err != nil || !bytes.Equal(live.Bytes(), replay.Bytes()) {
		t.Fatal("checkpoint restore request changed", err)
	}
	values["stop_checkpoint_request.golden.json"] = append(live.Bytes(), '\n')
	evidence, _ := codec.MarshalStable(struct {
		NativeRevision  uint64            `json:"native_revision"`
		ContextRevision uint64            `json:"context_revision"`
		Native          []json.RawMessage `json:"native"`
	}{7, footprint.Revision(), []json.RawMessage{native.Payload(), steer.Payload()}})
	values["stop_delivery.golden.json"] = append(evidence, '\n')
	return values
}
func TestStopHookNativeRequestCacheAndCheckpointGoldens(t *testing.T) {
	for name, got := range stopGoldenValues(t) {
		want, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("Stop golden differs %s", name)
		}
	}
}

func mustStopCachePolicy(t *testing.T, mode domain.AnthropicCacheMode) domain.CachePolicy {
	t.Helper()
	p, err := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
