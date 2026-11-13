package anthropic

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"reflect"
	"strings"
	"testing"
)

const stopTestTurn domain.TurnID = "01900000-0000-7000-8000-000000000001"

func stopTestFeedback(t *testing.T, index uint32) domain.StopFeedback {
	t.Helper()
	declaration, err := domain.NewStopHookDeclaration(domain.HookSourceUser, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: stopTestTurn, SampleIndex: index}, uint8(index), []domain.HookReference{declaration.Reference})
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.NewStopHookRunIdentity(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	reason, _ := domain.NewHookFeedbackCandidate("  补齐遗漏项\n")
	feedback, err := domain.NewStopFeedback(domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, declaration.Reference, reason)
	if err != nil {
		t.Fatal(err)
	}
	return feedback
}
func stopTestSample(turn domain.TurnID, text string) nativeHistoryEntry {
	sample := nativeHistoryEntry{Kind: nativeHistorySample, TurnID: turn, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
		{Type: blockTypeThinking, Thinking: "PRIVATE-THINKING", Signature: "PRIVATE-OPAQUE"},
		{Type: blockTypeText, Text: text},
	}}, Metadata: messageMetadata{ID: "message", Model: "model"}}
	return sample
}
func stopTestConversation(t *testing.T) (*Conversation, domain.InputAssociation, nativeHistoryEntry) {
	t.Helper()
	instance, err := New(Config{BaseURL: "http://127.0.0.1", APIKey: secret.New("test-key"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Error(err)
		}
	})
	conversation := testConversation(t, instance)
	user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "original task")
	association, _ := domain.NewInputAssociation(stopTestTurn, domain.InputInitial, 0, []domain.InputID{user.ID()})
	entry, err := userInputEntry(association, []domain.UserInput{user})
	if err != nil {
		t.Fatal(err)
	}
	conversation.history.commit(entry)
	conversation.history.commit(stopTestSample(stopTestTurn, "  同一答复\n"))
	return conversation, association, entry
}

func TestStopProjectionUsesExactSampleAndLeavesRequestsUnchanged(t *testing.T) {
	conversation, _, _ := stopTestConversation(t)
	first := domain.AssistantSampleReference{TurnID: stopTestTurn}
	input, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	before, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	text, err := conversation.ProjectAssistantSample(first)
	if err != nil || text.Reference() != first || text.Text() != "  同一答复\n" {
		t.Fatal("exact projection", err)
	}
	after, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
	if err != nil || !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("projection changed request", err)
	}
	for index := uint32(0); index < 2; index++ {
		feedback := stopTestFeedback(t, index)
		entry, err := stopFeedbackEntry(feedback)
		if err != nil {
			t.Fatal(err)
		}
		conversation.history.commit(entry)
		next := stopTestSample(stopTestTurn, "")
		next.Assistant.Content = next.Assistant.Content[:1]
		if index == 1 {
			next = stopTestSample(stopTestTurn, "  同一答复\n")
		}
		conversation.history.commit(next)
	}
	for index, want := range []string{"  同一答复\n", "", "  同一答复\n"} {
		reference := domain.AssistantSampleReference{TurnID: stopTestTurn, SampleIndex: uint32(index)}
		text, err := conversation.ProjectAssistantSample(reference)
		if err != nil || text.Reference() != reference || text.Text() != want {
			t.Fatalf("wrong sample %d: %q %v", index, text.Text(), err)
		}
	}
	for _, reference := range []domain.AssistantSampleReference{{TurnID: stopTestTurn, SampleIndex: 3}, {TurnID: "01900000-0000-7000-8000-000000000099"}} {
		if _, err := conversation.ProjectAssistantSample(reference); err == nil {
			t.Fatal("missing sample accepted")
		}
	}
}

func TestStopNativeContinuationCommitsBothSourcesAndRestoresExactly(t *testing.T) {
	for _, steer := range []bool{false, true} {
		t.Run(map[bool]string{false: "feedback-only", true: "with-steering"}[steer], func(t *testing.T) {
			conversation, initial, initialEntry := stopTestConversation(t)
			sample := conversation.history.snapshot()[1]
			feedback := stopTestFeedback(t, 0)
			candidate, _ := domain.NewStopFeedbackOnly(feedback)
			var association domain.InputAssociation
			if steer {
				user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000003", "  用户纠正\n")
				association, _ = domain.NewInputAssociation(stopTestTurn, domain.InputSteering, 1, []domain.InputID{user.ID()})
				candidate, _ = domain.NewStopWithSteering(feedback, association, []domain.UserInput{user})
			}
			prepared, err := conversation.PrepareStopContinuation(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if len(conversation.ProjectHistory().Turns[0].Messages) != 2 {
				t.Fatal("staged continuation visible")
			}
			if _, err := conversation.PrepareStopContinuation(candidate); err == nil {
				t.Fatal("second pending owner accepted")
			}
			view, footprint, err := prepared.Preview()
			if err != nil {
				t.Fatal(err)
			}
			wantRevision := uint64(3)
			if steer {
				wantRevision++
			}
			if view.Revision() != wantRevision || footprint.Revision() != wantRevision {
				t.Fatal("revision not based on actual entries")
			}
			sampling, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.CheckRequest(sampling); err != nil {
				t.Fatal("candidate request", err)
			}
			feedbackEnvelope, err := prepared.FeedbackEnvelope()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeNativeCommit(feedbackEnvelope)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Kind != nativeHistoryHookFeedback || decoded.FeedbackInput == nil || decoded.FeedbackInput.Role != "user" {
				t.Fatal("invalid native role")
			}
			frozen, err := entryStopFeedback(decoded)
			if err != nil || frozen != feedback {
				t.Fatal("frozen bytes changed", err)
			}
			initialEnvelope, err := encodeNativeCommit(initialEntry)
			if err != nil {
				t.Fatal(err)
			}
			sampleEnvelope, err := encodeNativeCommit(sample)
			if err != nil {
				t.Fatal(err)
			}
			r0, err := provider.NewInputRestoreRecord(initial, initialEnvelope)
			if err != nil {
				t.Fatal(err)
			}
			r1, err := provider.NewSampleRestoreRecord(stopTestTurn, sampleEnvelope)
			if err != nil {
				t.Fatal(err)
			}
			r2, err := provider.NewStopFeedbackRestoreRecord(feedback, feedbackEnvelope)
			if err != nil {
				t.Fatal(err)
			}
			records := []provider.RestoreRecord{r0, r1, r2}
			if envelope, ok, err := prepared.SteeringEnvelope(); err != nil {
				t.Fatal(err)
			} else if ok {
				record, err := provider.NewInputRestoreRecord(association, envelope)
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, record)
			}
			if err := prepared.Finalize(); err != nil {
				t.Fatal(err)
			}
			if !prepared.Finalized() || prepared.Finalize() == nil || prepared.Discard() == nil {
				t.Fatal("double resolution accepted")
			}
			actualFootprint, err := conversation.HistoryFootprint()
			if err != nil || actualFootprint != footprint {
				t.Fatal("finalized footprint differs", err)
			}
			restored := testConversation(t, conversation.provider)
			if err := restoreNativeHistory(context.Background(), "model", records, &restored.history); err != nil {
				t.Fatal("restore", err)
			}
			liveBody, err := conversation.compileSamplingRequest(conversation.history.snapshot(), sampling)
			if err != nil {
				t.Fatal(err)
			}
			restoredBody, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
			if err != nil || !bytes.Equal(liveBody.Bytes(), restoredBody.Bytes()) {
				t.Fatal("canonical request changed across restore", err)
			}
			if !bytes.Contains(liveBody.Bytes(), []byte("PRIVATE-OPAQUE")) {
				t.Fatal("opaque history lost")
			}
			history := restored.ProjectHistory()
			encoded, err := codec.MarshalStable(history)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte("PRIVATE-")) {
				t.Fatal("opaque data exposed in semantic view")
			}
			if !reflect.DeepEqual(history, conversation.ProjectHistory()) {
				t.Fatal("semantic restore differs")
			}
			messages := history.Turns[0].Messages
			if len(messages) != 3+map[bool]int{false: 0, true: 1}[steer] || messages[2].Role != domain.SemanticHook || messages[2].InputID != "" {
				t.Fatal("hook merged with real input")
			}
			if steer && (messages[3].Role != domain.SemanticUser || messages[3].Text != "  用户纠正\n") {
				t.Fatal("real correction reordered")
			}
			transcript, err := restored.ProjectTranscript()
			if err != nil {
				t.Fatal(err)
			}
			hooks := 0
			for _, item := range transcript.Items {
				if item.Kind == domain.TranscriptHook {
					hooks++
					if item.Position.Entry != 2 || item.InputID != "" || item.Text != feedback.Text {
						t.Fatal("wrong hook cursor")
					}
				}
			}
			if hooks != 1 {
				t.Fatal("hook transcript missing")
			}
			history.Turns[0].Messages[2].HookSource.Reference.ID = "changed"
			if restored.ProjectHistory().Turns[0].Messages[2].HookSource.Reference.ID == "changed" {
				t.Fatal("projection alias")
			}
			badFeedback := feedback
			badFeedback.Reference.ID = "forged"
			if badFeedback.Validate() == nil {
				t.Fatal("source changed without model bytes")
			}
			otherReason, _ := domain.NewHookFeedbackCandidate("different authorized reason")
			result := domain.HookRunResult{Identity: feedback.Identity, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
			forged, err := domain.NewStopFeedback(result, feedback.Reference, otherReason)
			if err != nil {
				t.Fatal(err)
			}
			forgedEnvelope, err := conversation.EncodeStopFeedback(forged)
			if err != nil {
				t.Fatal(err)
			}
			badRecord, err := provider.NewStopFeedbackRestoreRecord(feedback, forgedEnvelope)
			if err != nil {
				t.Fatal(err)
			}
			badRecords := append([]provider.RestoreRecord(nil), records...)
			badRecords[2] = badRecord
			if err := restoreNativeHistory(context.Background(), "model", badRecords, &nativeHistory{}); err == nil {
				t.Fatal("forged native feedback accepted")
			}
		})
	}
}

func TestStopNativeEntryRejectsElevatedRoleAndExtraSources(t *testing.T) {
	feedback := stopTestFeedback(t, 0)
	entry, err := stopFeedbackEntry(feedback)
	if err != nil {
		t.Fatal(err)
	}
	entry.FeedbackInput.Role = "assistant"
	if _, err := encodeNativeCommit(entry); err == nil {
		t.Fatal("elevated hook role accepted")
	}
	envelope, err := (&Conversation{}).EncodeStopFeedback(feedback)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"input_ids", "payload_version", "tool_call_id"} {
		bad := append(bytes.TrimSuffix(bytes.Clone(envelope.Payload()), []byte("}")), []byte(",\""+field+"\":\"forged\"}")...)
		altered, err := provider.NewNativeCommitEnvelope(envelope.Family(), envelope.Wire(), bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeNativeCommit(altered); err == nil {
			t.Fatal("extra source accepted", field)
		}
	}
	if strings.Contains(string(envelope.Payload()), "system") {
		t.Fatal("unexpected privilege")
	}
}
