package anthropic

import (
	"bytes"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"reflect"
	"testing"
)

func TestSteeringInputsPreviewCommitRestoreAndRequestEquivalence(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := instance.NewConversation().(*Conversation)
	turnID := domain.TurnID("01900000-0000-7000-8000-000000000010")
	ids := []domain.InputID{"01900000-0000-7000-8000-000000000011", "01900000-0000-7000-8000-000000000012", "01900000-0000-7000-8000-000000000013"}
	texts := []string{"inspect", "  preserve 中文\n", "also test\t"}
	values := make([]domain.UserInput, len(ids))
	for i, id := range ids {
		values[i], err = domain.NewUserInput(id, texts[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	initial, _ := domain.NewInputAssociation(turnID, domain.InputInitial, 0, ids[:1])
	prepared, err := conversation.PrepareUserInputs(initial, values[:1])
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := prepared.Preview()
	if err != nil || len(preview.History().Turns) != 1 || len(conversation.ProjectHistory().Turns) != 0 {
		t.Fatal("candidate leaked into committed history")
	}
	if err := prepared.Discard(); err != nil {
		t.Fatal(err)
	}
	if len(conversation.historySnapshot()) != 0 {
		t.Fatal("discard committed input")
	}
	prepared, err = conversation.PrepareUserInputs(initial, values[:1])
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	sample := nativeHistoryEntry{Kind: nativeHistorySample, TurnID: turnID, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
		{Type: blockTypeThinking, Thinking: "private", Signature: "opaque"},
		{Type: blockTypeRedactedThinking, RedactedData: "redacted"},
		{Type: blockTypeText, Text: "checking"},
		{Type: blockTypeToolUse, ID: "call-read", Name: "Read", Input: []byte(`{ "file_path": "a" }`)},
	}}, Metadata: messageMetadata{ID: "m-1", Model: "fixture-model"}}
	conversation.history.commit(sample)
	steering, _ := domain.NewInputAssociation(turnID, domain.InputSteering, 1, ids[1:])
	if candidate, err := conversation.PrepareUserInputs(steering, values[1:]); err == nil {
		candidate.Discard()
		t.Fatal("steering accepted before complete tool outputs")
	}
	conversation.history.commit(nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: turnID, ToolOutputs: nativeMessage{Role: roleUser, Content: []NativeItem{{Type: blockTypeToolResult, ToolUseID: "call-read", Content: `{"status":"success","code":"ok","content":"done"}`}}}})
	before, _ := conversation.HistoryFootprint()
	prepared, err = conversation.PrepareUserInputs(steering, values[1:])
	if err != nil {
		t.Fatal(err)
	}
	sampling, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.CheckRequest(sampling); err != nil {
		t.Fatal(err)
	}
	preview, candidateFootprint, err := prepared.Preview()
	if err != nil || len(preview.History().Turns) != 1 || len(preview.History().Turns[0].Messages) != 4 || preview.History().Turns[0].Messages[2].InputID != ids[1] || candidateFootprint.Revision() != before.Revision()+1 {
		t.Fatal("candidate association or footprint changed")
	}
	if len(conversation.ProjectHistory().Turns[0].Messages) != 2 {
		t.Fatal("steering became visible before finalize")
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	var envelopes []provider.NativeCommitEnvelope
	for _, entry := range conversation.historySnapshot() {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	restoredValue, err := instance.RestoreConversation(testRestoreRecords(t, envelopes))
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	liveTranscript, err := conversation.ProjectTranscript()
	if err != nil {
		t.Fatal(err)
	}
	restoredTranscript, err := restored.ProjectTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(liveTranscript, restoredTranscript) {
		t.Fatal("restored transcript differs")
	}
	if len(liveTranscript.Items) != 5 || liveTranscript.Items[2].Kind != domain.TranscriptCall || liveTranscript.Items[3].InputID != ids[1] || liveTranscript.Items[4].InputID != ids[2] || liveTranscript.Items[3].Position.Entry != 3 || liveTranscript.Items[3].TurnID != turnID {
		t.Fatalf("steering transcript order changed: %#v", liveTranscript)
	}

	liveBytes, err := compileMessagesRequest("fixture-model", 4096, conversation.historySnapshot(), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	restoredBytes, err := compileMessagesRequest("fixture-model", 4096, restored.historySnapshot(), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(liveBytes.Bytes(), restoredBytes.Bytes()) {
		t.Fatal("restored request differs from uninterrupted request")
	}
	liveFootprint, _ := conversation.HistoryFootprint()
	restoredFootprint, _ := restored.HistoryFootprint()
	if liveFootprint != restoredFootprint {
		t.Fatal("restored footprint changed")
	}
	shape := buildMessagesRequest("fixture-model", 4096, conversation.historySnapshot(), nil, testAnthropicToolView(t))
	if len(shape.Messages) != 5 || shape.Messages[2].Content[0].Type != blockTypeToolResult || shape.Messages[3].Content[0].Text != texts[1] || shape.Messages[4].Content[0].Text != texts[2] || shape.Messages[1].Content[0].Signature != "opaque" || shape.Messages[1].Content[1].RedactedData != "redacted" {
		t.Fatal("native steering order or opaque content changed")
	}
	// 输入身份只参与恢复关联，不得进入 Provider wire 或缓存内容。
	changed := conversation.historySnapshot()
	changed[len(changed)-1].InputMetadata.InputIDs[0] = "01900000-0000-7000-8000-000000000099"
	changedBytes, err := compileMessagesRequest("fixture-model", 4096, changed, nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(liveBytes.Bytes(), changedBytes.Bytes()) {
		t.Fatal("input identity polluted request bytes")
	}
	assertCompactionAfterToolSteering(t, instance, conversation, sampling, envelopes)
	records := testRestoreRecords(t, envelopes)
	wrong, _ := domain.NewInputAssociation(turnID, domain.InputSteering, 2, ids[1:])
	records[len(records)-1], _ = provider.NewInputRestoreRecord(wrong, envelopes[len(envelopes)-1])
	if _, err := instance.RestoreConversation(records); err == nil {
		t.Fatal("mismatched restore association accepted")
	}
}

func TestUserInputsCanonicalGolden(t *testing.T) {
	a, _ := domain.NewInputAssociation("01900000-0000-7000-8000-000000000010", domain.InputInitial, 0, []domain.InputID{"01900000-0000-7000-8000-000000000011"})
	input, _ := domain.NewUserInput(a.IDs()[0], "  hello\n")
	var conversation Conversation
	envelope, err := conversation.EncodeUserInputs(a, []domain.UserInput{input})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"user_inputs","user_inputs":[{"role":"user","content":[{"type":"text","text":"  hello\n"}]}],"input_metadata":{"turn_id":"01900000-0000-7000-8000-000000000010","placement":"initial","target_sample_index":0,"input_ids":["01900000-0000-7000-8000-000000000011"]}}`
	if string(envelope.Payload()) != want {
		t.Fatalf("input golden changed: %s", envelope.Payload())
	}
}
