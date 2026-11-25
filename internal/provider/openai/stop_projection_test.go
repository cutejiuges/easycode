package openai

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

const stopTestTurn domain.TurnID = "01900000-0000-7000-8000-000000000001"

func stopTestSample(turn domain.TurnID, text string) nativeHistoryEntry {
	sample := nativeHistoryEntry{Kind: nativeHistorySample, TurnID: turn, Outputs: []NativeItem{
		{Type: "reasoning", EncryptedContent: "PRIVATE-OPAQUE", ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "PRIVATE-SUMMARY"}}},
		{Type: "message", Role: "assistant", Phase: "final", Content: []ContentPart{{Type: "output_text", Text: text}}},
	}}
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
	conversation.history.Append(entry)
	conversation.history.Append(stopTestSample(stopTestTurn, "  同一答复\n"))
	return conversation, association, entry
}

func TestStopNativeContinuationCommitsBothSourcesAndRestoresExactly(t *testing.T) {
	for _, steer := range []bool{false, true} {
		t.Run(map[bool]string{false: "feedback-only", true: "with-steering"}[steer], func(t *testing.T) {
			conversation, initial, initialEntry := stopTestConversation(t)
			sample := conversation.history.Entries()[1]
			sampling, err := testSamplingInput(t, conversation.provider.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			initialEnvelope, err := encodeNativeCommit(initialEntry)
			if err != nil {
				t.Fatal(err)
			}
			sampleEnvelope, err := encodeNativeCommit(sample)
			if err != nil {
				t.Fatal(err)
			}
			testsupport.CheckStopContinuation(t, conversation, initial, initialEnvelope, sampleEnvelope, sampling, steer,
				func(records []provider.RestoreRecord) (testsupport.StopConversation, error) {
					restored, err := testRestoreAfterPreview(t, conversation.provider, "01900000-0000-7000-8000-000000000001", records)
					if err != nil {
						return nil, err
					}
					return restored.(*Conversation), nil
				}, func(envelope provider.NativeCommitEnvelope) (domain.StopFeedback, error) {
					decoded, err := decodeNativeCommit(envelope)
					if err != nil {
						return domain.StopFeedback{}, err
					}
					if decoded.Kind != nativeHistoryHookFeedback || decoded.FeedbackInput == nil || decoded.FeedbackInput.Role != "user" || decoded.FeedbackInput.Type != "message" {
						t.Fatal("invalid native role")
					}
					return entryStopFeedback(decoded)
				})

		})
	}
}
