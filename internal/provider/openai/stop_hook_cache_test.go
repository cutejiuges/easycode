package openai

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
)

// stopGoldenValues 固定两次反馈、真实 Skill 纠正、opaque 和完整窗口摘要的请求证据。
func stopGoldenValues(t *testing.T) map[string][]byte { return stopGoldenValuesWithFiles(t, false) }

func stopGoldenValuesWithFiles(t *testing.T, includeFiles bool) map[string][]byte {
	t.Helper()
	c, initial, initialEntry := stopTestConversation(t)
	initialNative, _ := encodeNativeCommit(initialEntry)
	sampleNative, _ := encodeNativeCommit(c.history.Entries()[1])
	appendSample := func(text string) provider.NativeCommitEnvelope {
		sample := stopTestSample(stopTestTurn, text)
		c.history.Append(sample)
		envelope, err := encodeNativeCommit(sample)
		if err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	native, steer, feedback2, records := testsupport.StopGoldenHistory(t, c, initial, initialNative, sampleNative, includeFiles, appendSample)
	restored := testConversation(t, c.provider)
	if _, err := restoreNativeHistory(context.Background(), "model", records, &restored.history); err != nil {
		t.Fatal(err)
	}
	sampling, err := testSamplingInput(t, c.provider.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string][]byte{"stop_feedback_native.golden.json": append(native.Payload(), '\n')}
	for _, policy := range []domain.CachePolicy{domain.DefaultCachePolicy(), mustStopCachePolicy(t)} {
		c.provider.config.CachePolicy = policy
		request, err := c.compileSamplingRequest(c.history.Entries(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		again, err := restored.compileSamplingRequest(restored.history.Entries(), sampling)
		if err != nil || !bytes.Equal(request.Bytes(), again.Bytes()) {
			t.Fatal("restored request differs", err)
		}
		_, original, err := c.ProjectContextWindow()
		if err != nil {
			t.Fatal(err)
		}
		_, actual, err := restored.ProjectContextWindow()
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
		name := string(policy.Key())
		values["stop_"+name+"_request.golden.json"] = append(body, '\n')
		for _, mode := range []string{"full", "incremental"} {
			previous, offset := "", 0
			if mode == "incremental" {
				previous = "response-fixture"
				offset = 3
			}
			wire, err := websocketBody(request, previous, offset)
			if err != nil {
				t.Fatal(err)
			}
			values["stop_"+name+"_ws_"+mode+".golden.json"] = append(wire.Bytes(), '\n')
		}
	}
	// 后续摘要覆盖完整旧窗口；已消费的反馈进入摘要来源，完整 transcript 永不丢失。
	envelope := appendSample("final answer")
	record, _ := provider.NewSampleRestoreRecord(stopTestTurn, envelope)
	records = append(records, record)
	snapshot := c.history.Snapshot()
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
	snapshot.Entries[5] = changedEntry
	sourceAfter, err := nativeCompactionSource(snapshot, "model")
	if err != nil || sourceBefore.Digest() == sourceAfter.Digest() {
		t.Fatal("compaction digest omitted delivered feedback", err)
	}
	before := c.ProjectHistory()
	transcript, _, err := c.ProjectTranscriptFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := provider.NewCompactionInput(sampling, "preserve completed checks", 4096)
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	request, err := c.compileCompactionRequest(c.history.Snapshot(), compact)
	if err != nil {
		t.Fatal(err)
	}
	values["stop_compaction_request.golden.json"] = append(request.Bytes(), '\n')
	generated := stopTestSample(stopTestTurn, "goal; completed tests; next step")
	checkpoint, err := c.prepareCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000004", preview.Source(), generated, syntheticRequestReceipt(domain.RequestSummary), domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	values["stop_checkpoint_request.golden.json"], values["stop_delivery.golden.json"] = testsupport.StopCheckpointGolden(t, c, checkpoint, records, sampling, native, steer, before, transcript)
	return values
}
func TestStopHookNativeRequestCacheAndCheckpointGoldens(t *testing.T) {
	testsupport.CheckStopGoldens(t, stopGoldenValues(t), false)
}

func mustStopCachePolicy(t *testing.T) domain.CachePolicy {
	t.Helper()
	p, err := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIHTTPSSE)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFileStopNativeRequestCacheAndCheckpointGoldens(t *testing.T) {
	testsupport.CheckStopGoldens(t, stopGoldenValuesWithFiles(t, true), true)
}
