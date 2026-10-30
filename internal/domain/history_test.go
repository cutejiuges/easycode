package domain

import (
	"easycode/internal/codec"
	"testing"
)

func TestSemanticHistoryViewStableJSONShape(t *testing.T) {
	id := InputID("01900000-0000-7000-8000-000000000001")
	turnID := TurnID("01900000-0000-7000-8000-000000000002")
	user, err := NewSemanticUserMessage(id, "first")
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := NewSemanticAssistantMessage("answer one")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := NewSemanticTurn(turnID, []SemanticMessage{user, assistant})
	if err != nil {
		t.Fatal(err)
	}
	view := SemanticHistoryView{Provider: ProviderAnthropic, Turns: []SemanticTurn{turn}}
	encoded, err := codec.MarshalStable(view)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"provider":"anthropic","turns":[{"turn_id":"01900000-0000-7000-8000-000000000002","messages":[{"role":"user","input_id":"01900000-0000-7000-8000-000000000001","text":"first"},{"role":"assistant","text":"answer one"}]}]}`
	if string(encoded) != want {
		t.Fatalf("semantic history JSON: got %s want %s", encoded, want)
	}
	copy := view.Clone()
	copy.Turns[0].Messages[0].Text = "mutated"
	if view.Turns[0].Messages[0].Text != "first" {
		t.Fatal("clone shares messages")
	}
	if _, err := NewSemanticTurn(turnID, []SemanticMessage{user, user}); err == nil {
		t.Fatal("duplicate input accepted")
	}
	if _, err := NewSemanticTurn("", []SemanticMessage{user}); err == nil {
		t.Fatal("missing turn accepted")
	}
	if _, err := NewSemanticUserMessage("", "first"); err == nil {
		t.Fatal("missing input accepted")
	}
	if (SemanticMessage{Role: SemanticAssistant, InputID: id, Text: "answer"}).Validate() == nil {
		t.Fatal("assistant input identity accepted")
	}
}

func TestSemanticHistoryViewRepresentsEmptyHistoryWithNonNilTurns(t *testing.T) {
	view := SemanticHistoryView{Provider: ProviderOpenAI, Turns: make([]SemanticTurn, 0)}
	encoded, err := codec.MarshalStable(view)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"provider":"openai","turns":[]}` {
		t.Fatalf("empty semantic history JSON: %s", encoded)
	}
}
