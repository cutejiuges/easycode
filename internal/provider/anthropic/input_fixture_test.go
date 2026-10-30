package anthropic

import (
	"context"
	"crypto/sha256"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
	"fmt"
	"testing"
)

func testInputID(text string) domain.InputID {
	sum := sha256.Sum256([]byte(text))
	return domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%x", sum[:6]))
}
func testInputEntry(t *testing.T, user nativeMessage) nativeHistoryEntry {
	t.Helper()
	id := testInputID(projectMessageText(user, roleUser))
	a, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := provider.NewInputMetadata(a)
	if err != nil {
		t.Fatal(err)
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, TurnID: a.TurnID(), InputMetadata: &metadata, UserInputs: []nativeMessage{user}}
}
func testAssociatedHistory(entries []nativeHistoryEntry) []nativeHistoryEntry {
	var turnID domain.TurnID
	for i := range entries {
		if entries[i].Kind == nativeHistoryUserInputs {
			turnID = entries[i].TurnID
		}
		entries[i].TurnID = turnID
	}
	return entries
}
func testCommitHistory(t *testing.T, history *nativeHistory, entries ...nativeHistoryEntry) {
	t.Helper()
	entries = testAssociatedHistory(entries)
	for _, entry := range entries {
		history.commit(entry)
	}
}
func testSemanticTurn(user, assistant string) domain.SemanticTurn {
	id := testInputID(user)
	messages := []domain.SemanticMessage{{Role: domain.SemanticUser, InputID: id, Text: user}}
	if assistant != "" {
		messages = append(messages, domain.SemanticMessage{Role: domain.SemanticAssistant, Text: assistant})
	}
	return domain.SemanticTurn{TurnID: domain.TurnID(id), Messages: messages}
}
func testCommitUserInput(t *testing.T, conversation *Conversation, text string) {
	t.Helper()
	id := testInputID(text)
	user, err := domain.NewUserInput(id, text)
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := conversation.PrepareUserInputs(a, []domain.UserInput{user})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
}
func testRestoreRecords(t *testing.T, envelopes []provider.NativeCommitEnvelope) []provider.RestoreRecord {
	t.Helper()
	var turnID domain.TurnID
	records := make([]provider.RestoreRecord, 0, len(envelopes))
	for _, envelope := range envelopes {
		entry, err := decodeNativeCommit(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var record provider.RestoreRecord
		switch entry.Kind {
		case nativeHistoryUserInputs:
			a, e := entry.InputMetadata.Association()
			if e != nil {
				t.Fatal(e)
			}
			turnID = a.TurnID()
			record, err = provider.NewInputRestoreRecord(a, envelope)
		case nativeHistorySample:
			record, err = provider.NewSampleRestoreRecord(turnID, envelope)
		case nativeHistoryToolOutputs:
			record, err = provider.NewToolOutputRestoreRecord(turnID, envelope)
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func testConsumeStream(t *testing.T, conversation *Conversation, ctx context.Context, cancel context.CancelFunc, user *nativeMessage, catalog tool.CatalogSnapshot, stream <-chan transport.SSEMessage, output chan<- provider.StreamEvent) {
	t.Helper()
	if user != nil {
		conversation.history.commit(testInputEntry(t, *user))
	}
	conversation.consumeStream(ctx, cancel, catalog, stream, output)
}

func testSampleRestoreSequence(t *testing.T, envelopes ...provider.NativeCommitEnvelope) []provider.RestoreRecord {
	t.Helper()
	entry := testInputEntry(t, newUserMessage("first"))
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		t.Fatal(err)
	}
	a, err := entry.InputMetadata.Association()
	if err != nil {
		t.Fatal(err)
	}
	input, err := provider.NewInputRestoreRecord(a, envelope)
	if err != nil {
		t.Fatal(err)
	}
	records := []provider.RestoreRecord{input}
	for _, envelope := range envelopes {
		record, err := provider.NewSampleRestoreRecord(a.TurnID(), envelope)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}
