package openai

import (
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestFileInputFixedNativeBlocksPrepareDiscardFinalizeAndRestore(t *testing.T) {
	for _, skill := range []bool{false, true} {
		t.Run(fmt.Sprint(skill), func(t *testing.T) {
			instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			c := testConversation(t, instance)
			sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			restored := testsupport.CheckFileInput(t, c, sampling, skill, func(envelopes []provider.NativeCommitEnvelope) (*Conversation, error) {
				value, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, envelopes))
				if err != nil {
					return nil, err
				}
				return value.(*Conversation), nil
			})
			entries := restored.history.Entries()
			entries[0].InputMetadata.Files[0].Attachments = domain.FileAttachments{}
			if validateNativeHistory(entries) == nil || restored.ProjectHistory().Turns[0].Messages[0].Files.Len() != 2 {
				t.Fatal("clone leaked or invalid metadata accepted")
			}
		})
	}
}
