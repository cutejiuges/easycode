package openai

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
			want: domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{}},
		},
		{
			name: "ordered text across items and parts",
			history: []nativeTurn{
				{
					User: NativeItem{Type: "message", Role: "user", Content: []ContentPart{
						{Type: "input_text", Text: "first\n"},
						{Type: "input_image", Text: "ignored-image"},
						{Type: "input_text", Text: "question"},
					}},
					Outputs: []NativeItem{
						{Type: "reasoning", ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "private summary"}}, EncryptedContent: "opaque-encrypted"},
						{Type: "message", ID: "msg-1", Role: "assistant", Phase: "final", Content: []ContentPart{
							{Type: "output_text", Text: "answer "},
							{Type: "refusal", Text: "ignored-refusal"},
						}},
						{Type: "future_item", Raw: []byte(`{"type":"future_item","content":[{"type":"output_text","text":"ignored-future"}]}`)},
						{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "one"}}},
					},
				},
				{
					User: NewUserItem("second"),
					Outputs: []NativeItem{{
						Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "answer two"}},
					}},
				},
			},
			want: domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{
				{UserText: "first\nquestion", AssistantText: "answer one"},
				{UserText: "second", AssistantText: "answer two"},
			}},
		},
		{
			name: "empty assistant text and strict item filters",
			history: []nativeTurn{
				{
					User: NewUserItem("visible"),
					Outputs: []NativeItem{
						{Type: "reasoning", Content: []ContentPart{{Type: "output_text", Text: "wrong item type"}}},
						{Type: "message", Role: "user", Content: []ContentPart{{Type: "output_text", Text: "wrong role"}}},
						{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "input_text", Text: "wrong part"}}},
					},
				},
				{
					User: NativeItem{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "input_text", Text: "wrong user role"}}},
					Outputs: []NativeItem{{
						Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "visible answer"}},
					}},
				},
			},
			want: domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{
				{UserText: "visible", AssistantText: ""},
				{UserText: "", AssistantText: "visible answer"},
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
			conversation.history.commit(
				NewUserItem(fmt.Sprintf("user-%d", index)),
				[]NativeItem{{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: fmt.Sprintf("assistant-%d", index)}}}},
			)
			runtime.Gosched()
		}
	}()
	close(start)

	for {
		projection := conversation.ProjectHistory()
		assertCompleteOpenAIProjection(t, projection)
		select {
		case <-done:
			final := conversation.ProjectHistory()
			assertCompleteOpenAIProjection(t, final)
			if len(final.Turns) != 200 {
				t.Fatalf("final turn count: %d", len(final.Turns))
			}
			return
		default:
			runtime.Gosched()
		}
	}
}

func assertCompleteOpenAIProjection(t *testing.T, projection domain.SemanticHistoryView) {
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
	conversation.history.commit(NewUserItem("hello\nworld"), []NativeItem{
		{
			Type:             "reasoning",
			ID:               "reasoning-secret",
			Phase:            "analysis",
			ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "private-summary"}},
			EncryptedContent: "opaque-encrypted-content",
		},
		{Type: "future_item", Raw: []byte(`{"type":"future_item","secret":"opaque-extension"}`)},
		{Type: "message", ID: "msg-secret", Role: "assistant", Phase: "final", Content: []ContentPart{{Type: "output_text", Text: "visible answer"}}},
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
	for _, opaque := range []string{"reasoning-secret", "analysis", "private-summary", "opaque-encrypted-content", "opaque-extension", "msg-secret", "final"} {
		if strings.Contains(string(first), opaque) {
			t.Fatalf("projection leaked %q: %s", opaque, first)
		}
	}
}

func TestProjectHistoryIsIndependentAndDoesNotChangeRequest(t *testing.T) {
	conversation := &Conversation{}
	reasoningRaw := []byte(`{"type":"reasoning","id":"reasoning-1","summary":[{"type":"summary_text","text":"private"}],"encrypted_content":"opaque-encrypted"}`)
	conversation.history.commit(NewUserItem("first"), []NativeItem{
		{Type: "reasoning", ID: "reasoning-1", EncryptedContent: "opaque-encrypted", Raw: reasoningRaw},
		{Type: "message", ID: "msg-1", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "answer"}}},
	})

	before, err := compileResponsesRequest("gpt-test", conversation.history.snapshot(), NewUserItem("second"))
	if err != nil {
		t.Fatalf("compile request before projection: %v", err)
	}
	beforeSegment, err := contextplan.NewSegment("openai-responses-request", contextplan.StabilityTurnStable, "v1", before)
	if err != nil {
		t.Fatalf("fingerprint request before projection: %v", err)
	}

	view := conversation.ProjectHistory()
	view.Provider = domain.ProviderAnthropic
	view.Turns[0].UserText = "changed"
	view.Turns[0].AssistantText = "changed"
	view.Turns = append(view.Turns, domain.SemanticTurn{UserText: "injected"})

	afterView := conversation.ProjectHistory()
	wantView := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{{UserText: "first", AssistantText: "answer"}}}
	assertSemanticHistory(t, afterView, wantView)
	after, err := compileResponsesRequest("gpt-test", conversation.history.snapshot(), NewUserItem("second"))
	if err != nil {
		t.Fatalf("compile request after projection: %v", err)
	}
	afterSegment, err := contextplan.NewSegment("openai-responses-request", contextplan.StabilityTurnStable, "v1", after)
	if err != nil {
		t.Fatalf("fingerprint request after projection: %v", err)
	}
	if beforeSegment.Fingerprint() != afterSegment.Fingerprint() || string(beforeSegment.CanonicalJSON()) != string(afterSegment.CanonicalJSON()) {
		t.Fatalf("projection changed request:\n before: %s\n after: %s", beforeSegment.CanonicalJSON(), afterSegment.CanonicalJSON())
	}
	afterRequest := buildResponsesRequest("gpt-test", conversation.history.snapshot(), NewUserItem("second"))
	if len(afterRequest.Input) != 4 || afterRequest.Input[1].EncryptedContent != "opaque-encrypted" || string(afterRequest.Input[1].Raw) != string(reasoningRaw) {
		t.Fatalf("projection changed reasoning replay: %#v", afterRequest.Input)
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
