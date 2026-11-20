package openai

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestCheckpointWindowAndRestoredRequest(t *testing.T) {
	auto, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", 1)
	for _, scope := range []domain.CompactionScope{domain.ManualCompactionScope(), auto} {
		for _, key := range []domain.OpenAICacheKeyMode{domain.OpenAICacheOmit, domain.OpenAICacheThread} {
			policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, domain.OpenAIHTTPSSE)
			t.Run(string(scope.Trigger)+"/"+string(key), func(t *testing.T) { testCheckpointWindowAndRestoredRequest(t, scope, policy) })
		}
	}
}

func testCheckpointWindowAndRestoredRequest(t *testing.T, scope domain.CompactionScope, policy domain.CachePolicy) {
	instance, err := New(Config{CachePolicy: policy, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem(strings.Repeat("archived work ", 100))), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", EncryptedContent: "old-opaque"}, {Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "old answer"}}}}}})
	envelopes := make([]provider.NativeCommitEnvelope, 0)
	for _, entry := range entries {
		conversation.history.Append(entry)
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	records := testRestoreRecords(t, envelopes)
	full := conversation.ProjectHistory()
	fullTranscript, _, err := conversation.ProjectTranscriptFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	compactInput, err := provider.NewCompactionInput(input, "preserve test failures", 4096)
	if err != nil {
		t.Fatal(err)
	}
	preview, preflightContext, err := conversation.PreflightCompaction(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	request, err := conversation.compileCompactionRequest(conversation.history.Snapshot(), compactInput)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := "testdata/compaction_request.golden.json"
	if policy != domain.DefaultCachePolicy() {
		goldenPath = "testdata/cache_thread_compaction_request.golden.json"
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(request.Bytes(), bytes.TrimSpace(golden)) {
		t.Fatal("compaction request golden mismatch")
	}

	var wire responsesRequest
	if err := codec.Unmarshal(request.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ToolChoice != "none" || wire.MaxOutputTokens != 4096 || len(wire.Tools) != 10 || !strings.Contains(string(request.Bytes()), "preserve test failures") {
		t.Fatal("summary request protection or sources missing")
	}
	generation := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", EncryptedContent: "summary-generation-only"}, {Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "goal; completed tests; next task"}}}}}
	prepared, err := conversation.prepareCheckpoint(scope, "01900000-0000-7000-8000-000000000001", preview.Source(), generation, syntheticRequestReceipt(domain.RequestSummary), domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	testOfflineUsageEvidence(t, instance, prepared, checkpoint)
	_, before, _ := conversation.ProjectContextWindow()
	if before.Revision() != 2 {
		t.Fatal("staging changed committed revision")
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	captured, capturedFootprint, err := preflightContext()
	if err != nil || captured.Revision() != preview.Source().ContextRevision() ||
		capturedFootprint != before || !reflect.DeepEqual(captured.History(), full) {
		t.Fatal("preflight projection did not retain its original committed window", err)
	}
	afterTranscript, _, err := conversation.ProjectTranscriptFrom(0)
	if err != nil || !reflect.DeepEqual(fullTranscript, afterTranscript) {
		t.Fatal("checkpoint changed native transcript ordinals")
	}
	_, after, _ := conversation.ProjectContextWindow()
	if after.Revision() != 3 || !reflect.DeepEqual(full, conversation.ProjectHistory()) {
		t.Fatal("checkpoint changed full history or wrong revision")
	}
	if _, _, err := conversation.PreflightCompaction(compactInput); err == nil {
		t.Fatal("repeated compaction without increments accepted")
	}
	record, err := provider.NewCheckpointRestoreRecord(checkpoint, domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, record)
	next := testInputEntry(t, NewUserItem("continue"))
	conversation.history.Append(next)
	envelope, err := encodeNativeCommit(next)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})...)
	restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	a, err := conversation.compileSamplingRequest(conversation.history.Entries(), input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := restored.compileSamplingRequest(restored.history.Entries(), input)
	if err != nil {
		t.Fatal(err)
	}
	if (scope.Trigger == domain.CompactionAuto) != strings.Contains(string(a.Bytes()), "Continue the existing task directly") {
		t.Fatal("continuation scope lost")
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("restored canonical request differs")
	}
	if strings.Contains(string(a.Bytes()), "old-opaque") || strings.Contains(string(a.Bytes()), "summary-generation-only") || strings.Contains(string(a.Bytes()), "archived work") {
		t.Fatal("archived history or generation provenance entered request")
	}
	if !strings.Contains(string(a.Bytes()), "goal; completed tests; next task") || !strings.Contains(string(a.Bytes()), "continue") {
		t.Fatal("active summary or increment missing")
	}
	av, _ := conversation.ProjectActiveContext()
	bv, _ := restored.ProjectActiveContext()
	if !reflect.DeepEqual(av, bv) || len(av.History().Turns) != 1 {
		t.Fatal("active view restore mismatch")
	}
	_, af, _ := conversation.ProjectContextWindow()
	_, bf, _ := restored.ProjectContextWindow()
	if af != bf {
		t.Fatal("active footprint restore mismatch")
	}
	if !reflect.DeepEqual(conversation.ProjectHistory(), restored.ProjectHistory()) {
		t.Fatal("full history restore mismatch")
	}
	// 第二次 checkpoint 必须绑定前一摘要和新增输入，不能重新覆盖旧窗口来源。
	secondPreview, _, err := conversation.PreflightCompaction(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	if secondPreview.Source().Previous().ID() != checkpoint.ID() {
		t.Fatal("predecessor lost")
	}
	second, err := conversation.prepareCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000002", secondPreview.Source(), generation, syntheticRequestReceipt(domain.RequestSummary), domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	secondCheckpoint, _ := second.Checkpoint()
	if err := second.Finalize(); err != nil {
		t.Fatal(err)
	}
	secondRecord, _ := provider.NewCheckpointRestoreRecord(secondCheckpoint, domain.NewNoTaskProgress())
	records = append(records, secondRecord)
	restoredValue, err = testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	_, af, _ = conversation.ProjectContextWindow()
	_, bf, _ = restoredValue.ProjectContextWindow()
	if af != bf || af.Revision() != 5 {
		t.Fatal("second checkpoint revision mismatch")
	}
	bad := append([]provider.RestoreRecord{}, records...)
	bad[len(bad)-1] = record
	if value, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", bad); err == nil || value != nil {
		t.Fatal("stale predecessor accepted")
	}
}

func assertCompactionAfterToolSteering(t *testing.T, instance *Provider, conversation *Conversation, sampling provider.TurnInput, envelopes []provider.NativeCommitEnvelope, outputs []tool.ModelOutput) {
	t.Helper()
	records := testRestoreRecords(t, envelopes, outputs)
	original, _, _ := conversation.ProjectTranscriptFrom(0)
	input, _ := provider.NewCompactionInput(sampling, "preserve tool observations", 4096)
	for iteration, id := range []domain.CompactionID{"01900000-0000-7000-8000-000000000080", "01900000-0000-7000-8000-000000000081"} {
		preview, _, err := conversation.PreflightCompaction(input)
		if err != nil {
			t.Fatal(err)
		}
		scope, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", uint32(iteration+1))
		candidate, err := conversation.prepareCheckpoint(scope, id, preview.Source(), nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", EncryptedContent: "summary-opaque"}, {Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "Preserve goal and tests"}}}}}, syntheticRequestReceipt(domain.RequestSummary), domain.NewNoTaskProgress())
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, _ := candidate.Checkpoint()
		if err := candidate.Finalize(); err != nil {
			t.Fatal(err)
		}
		record, _ := provider.NewCheckpointRestoreRecord(checkpoint, domain.NewNoTaskProgress())
		records = append(records, record)
		restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
		if err != nil {
			t.Fatal(err)
		}
		restored := restoredValue.(*Conversation)
		live, err := conversation.compileSamplingRequest(conversation.history.Entries(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := restored.compileSamplingRequest(restored.history.Entries(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(live.Bytes(), recovered.Bytes()) {
			t.Fatal("tool/steering checkpoint request changed")
		}
		liveView, _ := conversation.ProjectActiveContext()
		restoredView, _ := restored.ProjectActiveContext()
		_, liveFootprint, _ := conversation.ProjectContextWindow()
		_, restoredFootprint, _ := restored.ProjectContextWindow()
		if !reflect.DeepEqual(liveView, restoredView) || liveFootprint != restoredFootprint {
			t.Fatal("tool/steering window changed")
		}
		profile, _ := contextplan.NewProviderProfile(conversation.Family(), "fixture-model")
		catalog, _, _ := sampling.ToolCatalog()
		project, _ := domain.NewProjectInstructionsSnapshot(nil, 32<<10)
		liveInput, err := contextplan.NewPlanningInput(profile, catalog, project, liveView, liveFootprint, contextplan.NewSamplingInput(), contextplan.DisabledBudget())
		if err != nil {
			t.Fatal(err)
		}
		restoredInput, err := contextplan.NewPlanningInput(profile, catalog, project, restoredView, restoredFootprint, contextplan.NewSamplingInput(), contextplan.DisabledBudget())
		if err != nil {
			t.Fatal(err)
		}
		livePlan, err := contextplan.NewPlanner().Plan(liveInput)
		if err != nil {
			t.Fatal(err)
		}
		restoredPlan, err := contextplan.NewPlanner().Plan(restoredInput)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(livePlan.CachePlan(), restoredPlan.CachePlan()) {
			t.Fatal("restored checkpoint fingerprints changed")
		}

		transcript, _, _ := restored.ProjectTranscriptFrom(0)
		if iteration == 0 && !reflect.DeepEqual(original, transcript) {
			t.Fatal("tool cursor or frozen preview changed")
		}
		if iteration == 0 {
			// 已提交输入即使没有后续成功 sample，仍必须被下一次压缩覆盖。
			entry := testInputEntry(t, NewUserItem("failed turn input retained"))
			conversation.history.Append(entry)
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})...)
		}
	}

	// 两次压缩之后，工具闭环的同轮 steering 仍按原生顺序追加。
	initial := testInputEntry(t, NewUserItem("post checkpoint input"))
	tail := []nativeHistoryEntry{initial, nativeHistoryEntry{Kind: nativeHistorySample, TurnID: initial.TurnID, Outputs: []NativeItem{{Type: "function_call", ID: "fc-next", CallID: "call-next", Name: "Read", Arguments: `{"file_path":"next"}`}}}, nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: initial.TurnID, ToolOutputs: []NativeItem{{Type: "function_call_output", CallID: "call-next", Output: `{"status":"success","code":"ok","content":"done"}`}}}}
	for _, entry := range tail {
		conversation.history.Append(entry)
	}
	inputID := domain.InputID("01900000-0000-7000-8000-000000000090")
	association, _ := domain.NewInputAssociation(initial.TurnID, domain.InputSteering, 1, []domain.InputID{inputID})
	value, _ := domain.NewUserInput(inputID, "post checkpoint steering")
	prepared, err := conversation.PrepareUserInputs(association, []domain.UserInput{value})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	entries := conversation.history.Entries()
	tail = append(tail, entries[len(entries)-1])
	var tailEnvelopes []provider.NativeCommitEnvelope
	for _, entry := range tail {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		tailEnvelopes = append(tailEnvelopes, envelope)
	}
	records = append(records, testRestoreRecords(t, tailEnvelopes, []tool.ModelOutput{testReadOutput(t, "call-next", "next", "done")})...)
	restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	live, err := conversation.compileSamplingRequest(conversation.history.Entries(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.compileSamplingRequest(restored.history.Entries(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live.Bytes(), recovered.Bytes()) || !bytes.Contains(live.Bytes(), []byte("post checkpoint steering")) {
		t.Fatal("post-checkpoint steering request changed")
	}
	liveTranscript, _, _ := conversation.ProjectTranscriptFrom(0)
	restoredTranscript, _, _ := restored.ProjectTranscriptFrom(0)
	if !reflect.DeepEqual(liveTranscript, restoredTranscript) {
		t.Fatal("post-checkpoint native ordinal changed")
	}
}
