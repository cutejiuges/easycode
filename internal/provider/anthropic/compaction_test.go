package anthropic

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
)

func TestCheckpointWindowAndRestoredRequest(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := instance.NewConversation().(*Conversation)
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage(strings.Repeat("archived work ", 100))), compactionTestGeneration("old answer", "old-opaque")})
	envelopes := make([]provider.NativeCommitEnvelope, 0)
	for _, entry := range entries {
		conversation.history.commit(entry)
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	records := testRestoreRecords(t, envelopes)
	full := conversation.ProjectHistory()
	fullTranscript, err := conversation.ProjectTranscript()
	if err != nil {
		t.Fatal(err)
	}
	input, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	compactInput, err := provider.NewCompactionInput(input, "preserve test failures", 4096)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := conversation.PreflightCompaction(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	request, err := conversation.compileCompactionRequest(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/compaction_request.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(request.Bytes(), bytes.TrimSpace(golden)) {
		t.Fatal("compaction request golden mismatch")
	}

	var wire messagesRequest
	if err := codec.Unmarshal(request.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ToolChoice == nil || wire.ToolChoice.Type != "none" || wire.MaxTokens != 4096 || len(wire.Tools) != 7 || !strings.Contains(string(request.Bytes()), "preserve test failures") {
		t.Fatal("summary request protection or sources missing")
	}
	generation := compactionTestGeneration("goal; completed tests; next task", "summary-generation-only")
	prepared, err := conversation.prepareCheckpoint("01900000-0000-7000-8000-000000000001", preview.Source(), generation)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := conversation.HistoryFootprint()
	if before.Revision() != 2 {
		t.Fatal("staging changed committed revision")
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	afterTranscript, err := conversation.ProjectTranscript()
	if err != nil || !reflect.DeepEqual(fullTranscript, afterTranscript) {
		t.Fatal("checkpoint changed native transcript ordinals")
	}
	after, _ := conversation.HistoryFootprint()
	if after.Revision() != 3 || !reflect.DeepEqual(full, conversation.ProjectHistory()) {
		t.Fatal("checkpoint changed full history or wrong revision")
	}
	if _, err := conversation.PreflightCompaction(compactInput); err == nil {
		t.Fatal("repeated compaction without increments accepted")
	}
	record, err := provider.NewCheckpointRestoreRecord(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, record)
	next := testInputEntry(t, newUserMessage("continue"))
	conversation.history.commit(next)
	envelope, err := encodeNativeCommit(next)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})...)
	restoredValue, err := instance.RestoreConversation(records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	a, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := restored.compileSamplingRequest(restored.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
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
	af, _ := conversation.HistoryFootprint()
	bf, _ := restored.HistoryFootprint()
	if af != bf {
		t.Fatal("active footprint restore mismatch")
	}
	if !reflect.DeepEqual(conversation.ProjectHistory(), restored.ProjectHistory()) {
		t.Fatal("full history restore mismatch")
	}
	// 第二次 checkpoint 必须绑定前一摘要和新增输入，不能重新覆盖旧窗口来源。
	secondPreview, err := conversation.PreflightCompaction(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	if secondPreview.Source().Previous().ID() != checkpoint.ID() {
		t.Fatal("predecessor lost")
	}
	second, err := conversation.prepareCheckpoint("01900000-0000-7000-8000-000000000002", secondPreview.Source(), generation)
	if err != nil {
		t.Fatal(err)
	}
	secondCheckpoint, _ := second.Checkpoint()
	if err := second.Finalize(); err != nil {
		t.Fatal(err)
	}
	secondRecord, _ := provider.NewCheckpointRestoreRecord(secondCheckpoint)
	records = append(records, secondRecord)
	restoredValue, err = instance.RestoreConversation(records)
	if err != nil {
		t.Fatal(err)
	}
	af, _ = conversation.HistoryFootprint()
	bf, _ = restoredValue.HistoryFootprint()
	if af != bf || af.Revision() != 5 {
		t.Fatal("second checkpoint revision mismatch")
	}
	bad := append([]provider.RestoreRecord{}, records...)
	bad[len(bad)-1] = record
	if value, err := instance.RestoreConversation(bad); err == nil || value != nil {
		t.Fatal("stale predecessor accepted")
	}
}

func TestCompactionRejectsInvalidOutput(t *testing.T) {
	for _, text := range []string{"", " \n", string([]byte{0xff}), strings.Repeat("x", domain.MaxContextSummaryBytes+1)} {
		generation := compactionTestGeneration(text, "opaque")
		if _, err := compactionSummary(generation); err == nil {
			t.Fatal("invalid summary accepted")
		}
	}
	for _, kind := range []string{"refusal", "tool_use", "future_item"} {
		generation := compactionTestGeneration("answer", "opaque")
		generation.Assistant.Content = append(generation.Assistant.Content, NativeItem{Type: kind})
		if _, err := compactionSummary(generation); err == nil {
			t.Fatal("non-text response accepted")
		}
	}
	generation := compactionTestGeneration("Error is a valid description of the failed test", "opaque")
	if _, err := compactionSummary(generation); err != nil {
		t.Fatal("text inspected as control flow", err)
	}
}
func compactionTestGeneration(text, opaque string) nativeHistoryEntry {
	return nativeHistoryEntry{Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "thinking", Signature: opaque}, {Type: blockTypeRedactedThinking, RedactedData: "redacted"}, {Type: blockTypeText, Text: text}}}, Metadata: messageMetadata{ID: "message-summary", Model: "model", StopReason: optionalString{Known: true, Value: "end_turn"}}}
}

func TestCompactionRejectsIncompleteStop(t *testing.T) {
	for _, reason := range []string{"max_tokens", "refusal", "tool_use", "stop_sequence", "pause_turn", ""} {
		generation := compactionTestGeneration("answer", "signature")
		generation.Metadata.StopReason.Value = reason
		if _, err := compactionSummary(generation); err == nil {
			t.Fatalf("accepted stop reason %q", reason)
		}
	}
}

func assertCompactionAfterToolSteering(t *testing.T, instance *Provider, conversation *Conversation, sampling provider.TurnInput, envelopes []provider.NativeCommitEnvelope) {
	t.Helper()
	records := testRestoreRecords(t, envelopes)
	original, _ := conversation.ProjectTranscript()
	input, _ := provider.NewCompactionInput(sampling, "preserve tool observations", 4096)
	for iteration, id := range []domain.CompactionID{"01900000-0000-7000-8000-000000000080", "01900000-0000-7000-8000-000000000081"} {
		preview, err := conversation.PreflightCompaction(input)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := conversation.prepareCheckpoint(id, preview.Source(), compactionTestGeneration("Preserve goal and tests", "summary-opaque"))
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, _ := candidate.Checkpoint()
		if err := candidate.Finalize(); err != nil {
			t.Fatal(err)
		}
		record, _ := provider.NewCheckpointRestoreRecord(checkpoint)
		records = append(records, record)
		restoredValue, err := instance.RestoreConversation(records)
		if err != nil {
			t.Fatal(err)
		}
		restored := restoredValue.(*Conversation)
		live, err := conversation.compileSamplingRequest(conversation.history.snapshot(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(live.Bytes(), recovered.Bytes()) {
			t.Fatal("tool/steering checkpoint request changed")
		}
		liveView, _ := conversation.ProjectActiveContext()
		restoredView, _ := restored.ProjectActiveContext()
		liveFootprint, _ := conversation.HistoryFootprint()
		restoredFootprint, _ := restored.HistoryFootprint()
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

		transcript, _ := restored.ProjectTranscript()
		if iteration == 0 && !reflect.DeepEqual(original, transcript) {
			t.Fatal("tool cursor or frozen preview changed")
		}
		if iteration == 0 {
			// 已提交输入即使没有后续成功 sample，仍必须被下一次压缩覆盖。
			entry := testInputEntry(t, newUserMessage("failed turn input retained"))
			conversation.history.commit(entry)
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})...)
		}
	}

	// 两次压缩之后，工具闭环的同轮 steering 仍按原生顺序追加。
	initial := testInputEntry(t, newUserMessage("post checkpoint input"))
	tail := []nativeHistoryEntry{initial, nativeHistoryEntry{Kind: nativeHistorySample, TurnID: initial.TurnID, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeToolUse, ID: "call-next", Name: "Read", Input: []byte(`{"file_path":"next"}`)}}}, Metadata: messageMetadata{ID: "next-message", Model: "fixture-model"}}, nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: initial.TurnID, ToolOutputs: nativeMessage{Role: roleUser, Content: []NativeItem{{Type: blockTypeToolResult, ToolUseID: "call-next", Content: `{"status":"success","code":"ok","content":"done"}`}}}}}
	for _, entry := range tail {
		conversation.history.commit(entry)
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
	entries := conversation.history.snapshot()
	tail = append(tail, entries[len(entries)-1])
	var tailEnvelopes []provider.NativeCommitEnvelope
	for _, entry := range tail {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		tailEnvelopes = append(tailEnvelopes, envelope)
	}
	records = append(records, testRestoreRecords(t, tailEnvelopes)...)
	restoredValue, err := instance.RestoreConversation(records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	live, err := conversation.compileSamplingRequest(conversation.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live.Bytes(), recovered.Bytes()) || !bytes.Contains(live.Bytes(), []byte("post checkpoint steering")) {
		t.Fatal("post-checkpoint steering request changed")
	}
	liveTranscript, _ := conversation.ProjectTranscript()
	restoredTranscript, _ := restored.ProjectTranscript()
	if !reflect.DeepEqual(liveTranscript, restoredTranscript) {
		t.Fatal("post-checkpoint native ordinal changed")
	}
}
