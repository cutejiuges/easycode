package domain

import (
	"testing"

	"easycode/internal/codec"
)

func TestSemanticHistoryViewStableJSONShape(t *testing.T) {
	view := SemanticHistoryView{
		Provider: ProviderAnthropic,
		Turns: []SemanticTurn{
			{UserText: "first", AssistantText: "answer one"},
			{UserText: "second", AssistantText: "answer two"},
		},
	}

	encoded, err := codec.MarshalStable(view)
	if err != nil {
		t.Fatalf("marshal semantic history: %v", err)
	}
	want := `{"provider":"anthropic","turns":[{"user_text":"first","assistant_text":"answer one"},{"user_text":"second","assistant_text":"answer two"}]}`
	if string(encoded) != want {
		t.Fatalf("semantic history JSON: got %s want %s", encoded, want)
	}
}

func TestSemanticHistoryViewRepresentsEmptyHistoryWithNonNilTurns(t *testing.T) {
	view := SemanticHistoryView{Provider: ProviderOpenAI, Turns: make([]SemanticTurn, 0)}
	encoded, err := codec.MarshalStable(view)
	if err != nil {
		t.Fatalf("marshal empty semantic history: %v", err)
	}
	if string(encoded) != `{"provider":"openai","turns":[]}` {
		t.Fatalf("empty semantic history JSON: %s", encoded)
	}
}
