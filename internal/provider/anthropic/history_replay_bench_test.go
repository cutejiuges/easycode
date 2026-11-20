package anthropic

import (
	"context"
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func BenchmarkRestoreToolHistory(b *testing.B) {
	for _, groups := range []int{64, 128} {
		b.Run(fmt.Sprintf("groups_%d", groups), func(b *testing.B) {
			records := replayToolHistoryFixture(b, groups)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := restoreNativeHistory(context.Background(), "fixture-model", records, &nativeHistory{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestIncrementalReplayPreservesNativeValidationPrecedence(t *testing.T) {
	records := replayToolHistoryFixture(t, 1)
	prefix := records[1]
	malformed, err := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	malformedRecord, err := provider.NewSampleRestoreRecord(prefix.TurnID(), malformed)
	if err != nil {
		t.Fatal(err)
	}
	_, decodeErr := decodeNativeCommit(malformed)
	if decodeErr == nil {
		t.Fatal("malformed fixture unexpectedly decoded")
	}
	for _, tc := range []struct {
		name    string
		records []provider.RestoreRecord
		want    string
	}{
		{"end", []provider.RestoreRecord{prefix}, "restore Anthropic native history: sample history position is invalid"},
		{"tool output", []provider.RestoreRecord{prefix, records[2]}, "sample history position is invalid"},
		{"malformed next record", []provider.RestoreRecord{prefix, malformedRecord}, "restore Anthropic native commit 2: " + decodeErr.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := restoreNativeHistory(context.Background(), "fixture-model", tc.records, &nativeHistory{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("restore error = %v; want %q", err, tc.want)
			}
		})
	}
}

func replayToolHistoryFixture(b testing.TB, groups int) []provider.RestoreRecord {
	b.Helper()
	records := make([]provider.RestoreRecord, 0, 3*groups)
	for i := range groups {
		callID := fmt.Sprintf("call-%d", i)
		inputID := testInputID(callID)
		turnID := domain.TurnID(inputID)
		user, err := domain.NewUserInput(inputID, "read file")
		if err != nil {
			b.Fatal(err)
		}
		association, err := domain.NewInputAssociation(turnID, domain.InputInitial, 0, []domain.InputID{inputID})
		if err != nil {
			b.Fatal(err)
		}
		input, err := (&Conversation{}).EncodeUserInputs(association, []domain.UserInput{user})
		if err != nil {
			b.Fatal(err)
		}
		inputRecord, err := provider.NewInputRestoreRecord(association, input)
		if err != nil {
			b.Fatal(err)
		}
		call := NativeItem{Type: blockTypeToolUse, ID: callID, Name: "Read", Input: []byte(`{"file_path":"README.md"}`)}
		sample, err := encodeNativeCommit(nativeHistoryEntry{
			Kind: nativeHistorySample, TurnID: turnID,
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{call}},
			Metadata:  messageMetadata{ID: callID, Model: "fixture-model"},
		})
		if err != nil {
			b.Fatal(err)
		}
		sampleRecord, err := provider.NewSampleRestoreRecord(turnID, sample)
		if err != nil {
			b.Fatal(err)
		}
		output := testReadOutput(b, callID, "README.md", "fixture")
		sampleRecord, err = sampleRecord.WithReadyFacts([]tool.Invocation{output.Evidence().Selection().Original()}, nil)
		if err != nil {
			b.Fatal(err)
		}
		item, err := encodeToolResult(call, output)
		if err != nil {
			b.Fatal(err)
		}
		outputEnvelope, err := encodeNativeCommit(nativeHistoryEntry{
			Kind: nativeHistoryToolOutputs, TurnID: turnID,
			ToolOutputs: nativeMessage{Role: roleUser, Content: []NativeItem{item}},
		})
		if err != nil {
			b.Fatal(err)
		}
		outputRecord, err := provider.NewToolOutputRestoreRecord(turnID, outputEnvelope, []tool.ModelOutput{output})
		if err != nil {
			b.Fatal(err)
		}
		records = append(records, inputRecord, sampleRecord, outputRecord)
	}
	return records
}
