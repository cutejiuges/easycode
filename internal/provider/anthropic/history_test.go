package anthropic

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
)

func TestProjectHistory(t *testing.T) {
	tests := []struct {
		name    string
		history []nativeTurn
		want    domain.SemanticHistoryView
	}{
		{
			name: "empty history",
			want: domain.SemanticHistoryView{Provider: domain.ProviderAnthropic, Turns: []domain.SemanticTurn{}},
		},
		{
			name: "ordered text and opaque blocks",
			history: []nativeTurn{
				{
					User: nativeMessage{Role: roleUser, Content: []NativeItem{
						{Type: blockTypeText, Text: "first\n"},
						{Type: blockTypeThinking, Thinking: "private-user"},
						{Type: blockTypeText, Text: "question"},
					}},
					Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
						{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"},
						{Type: blockTypeText, Text: "answer "},
						{Type: blockTypeRedactedThinking, RedactedData: "opaque-data"},
						{Type: blockTypeText, Text: "one"},
						{Type: "future_block", Raw: []byte(`{"type":"future_block","secret":"opaque"}`)},
					}},
					Metadata: messageMetadata{ID: "msg-private", Usage: rawUsage{InputTokens: optionalInt{Value: 7, Known: true}}},
				},
				{
					User:      newUserMessage("second"),
					Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "answer two"}}},
				},
			},
			want: domain.SemanticHistoryView{Provider: domain.ProviderAnthropic, Turns: []domain.SemanticTurn{
				{UserText: "first\nquestion", AssistantText: "answer one"},
				{UserText: "second", AssistantText: "answer two"},
			}},
		},
		{
			name: "empty assistant text and mismatched roles",
			history: []nativeTurn{
				{
					User:      newUserMessage("visible"),
					Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "hidden"}}},
				},
				{
					User:      nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "wrong user role"}}},
					Assistant: nativeMessage{Role: roleUser, Content: []NativeItem{{Type: blockTypeText, Text: "wrong assistant role"}}},
				},
			},
			want: domain.SemanticHistoryView{Provider: domain.ProviderAnthropic, Turns: []domain.SemanticTurn{
				{UserText: "visible", AssistantText: ""},
				{UserText: "", AssistantText: ""},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := projectHistory(test.history)
			assertSemanticHistory(t, got, test.want)
		})
	}
}

func TestProjectHistoryConcurrentCommitReturnsCompleteSnapshots(t *testing.T) {
	conversation := &Conversation{}
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-start
		for index := 0; index < 200; index++ {
			conversation.history.commit(nativeTurn{
				User:      newUserMessage(fmt.Sprintf("user-%d", index)),
				Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: fmt.Sprintf("assistant-%d", index)}}},
			})
			runtime.Gosched()
		}
	}()
	close(start)

	for {
		projection := conversation.ProjectHistory()
		assertCompleteAnthropicProjection(t, projection)
		select {
		case <-done:
			final := conversation.ProjectHistory()
			assertCompleteAnthropicProjection(t, final)
			if len(final.Turns) != 200 {
				t.Fatalf("final turn count: %d", len(final.Turns))
			}
			return
		default:
			runtime.Gosched()
		}
	}
}

func assertCompleteAnthropicProjection(t *testing.T, projection domain.SemanticHistoryView) {
	t.Helper()
	for index, turn := range projection.Turns {
		wantUser := fmt.Sprintf("user-%d", index)
		wantAssistant := fmt.Sprintf("assistant-%d", index)
		if turn.UserText != wantUser || turn.AssistantText != wantAssistant {
			t.Fatalf("partial turn %d: %#v", index, turn)
		}
	}
}

func TestProjectHistoryMatchesGoldenAndOmitsOpaqueData(t *testing.T) {
	conversation := &Conversation{}
	conversation.history.commit(nativeTurn{
		User: newUserMessage("hello\nworld"),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "private-thought", Signature: "opaque-signature"},
			{Type: blockTypeRedactedThinking, RedactedData: "opaque-data"},
			{Type: blockTypeText, Text: "visible answer"},
			{Type: "future_block", Raw: []byte(`{"type":"future_block","secret":"opaque-extension"}`)},
		}},
		Metadata: messageMetadata{ID: "msg-secret", Model: "claude-test", Usage: rawUsage{OutputTokens: optionalInt{Value: 9, Known: true}}},
	})

	first, err := codec.MarshalStable(conversation.ProjectHistory())
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	second, err := codec.MarshalStable(conversation.ProjectHistory())
	if err != nil {
		t.Fatalf("marshal repeated projection: %v", err)
	}
	want, err := os.ReadFile("testdata/history_projection.golden.json")
	if err != nil {
		t.Fatalf("read projection golden: %v", err)
	}
	if string(first) != strings.TrimSpace(string(want)) || string(second) != string(first) {
		t.Fatalf("projection golden mismatch:\n got: %s\nwant: %s", first, want)
	}
	for _, opaque := range []string{"private-thought", "opaque-signature", "opaque-data", "opaque-extension", "msg-secret", "claude-test", "output_tokens"} {
		if strings.Contains(string(first), opaque) {
			t.Fatalf("projection leaked %q: %s", opaque, first)
		}
	}
}

func TestProjectHistoryIsIndependentAndDoesNotChangeRequest(t *testing.T) {
	conversation := &Conversation{}
	conversation.history.commit(nativeTurn{
		User: newUserMessage("first"),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "private", Signature: "signature"},
			{Type: blockTypeText, Text: "answer"},
		}},
	})

	before := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, conversation.history.snapshot(), newUserMessage("second"))
	beforeSegment, err := contextplan.NewSegment("anthropic-messages-request", contextplan.StabilityTurnStable, "v1", before)
	if err != nil {
		t.Fatalf("fingerprint request before projection: %v", err)
	}

	view := conversation.ProjectHistory()
	view.Provider = domain.ProviderOpenAI
	view.Turns[0].UserText = "changed"
	view.Turns[0].AssistantText = "changed"
	view.Turns = append(view.Turns, domain.SemanticTurn{UserText: "injected"})

	afterView := conversation.ProjectHistory()
	wantView := domain.SemanticHistoryView{Provider: domain.ProviderAnthropic, Turns: []domain.SemanticTurn{{UserText: "first", AssistantText: "answer"}}}
	assertSemanticHistory(t, afterView, wantView)
	after := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, conversation.history.snapshot(), newUserMessage("second"))
	afterSegment, err := contextplan.NewSegment("anthropic-messages-request", contextplan.StabilityTurnStable, "v1", after)
	if err != nil {
		t.Fatalf("fingerprint request after projection: %v", err)
	}
	if beforeSegment.Fingerprint != afterSegment.Fingerprint || string(beforeSegment.CanonicalJSON) != string(afterSegment.CanonicalJSON) {
		t.Fatalf("projection changed request:\n before: %s\n after: %s", beforeSegment.CanonicalJSON, afterSegment.CanonicalJSON)
	}
	if after.Messages[1].Content[0].Signature != "signature" || after.Messages[1].Content[1].Text != "answer" {
		t.Fatalf("projection changed native history: %#v", after.Messages)
	}
}

func assertSemanticHistory(t *testing.T, got, want domain.SemanticHistoryView) {
	t.Helper()
	gotJSON, err := codec.MarshalStable(got)
	if err != nil {
		t.Fatalf("marshal got semantic history: %v", err)
	}
	wantJSON, err := codec.MarshalStable(want)
	if err != nil {
		t.Fatalf("marshal wanted semantic history: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("semantic history:\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}
