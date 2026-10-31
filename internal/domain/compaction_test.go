package domain

import (
	"strings"
	"testing"
)

func TestCompactionIdentityAndSummary(t *testing.T) {
	id, err := GenerateCompactionID()
	if err != nil || !id.Valid() {
		t.Fatalf("generate: %v", err)
	}
	if parsed, err := ParseCompactionID(string(id)); err != nil || parsed != id {
		t.Fatal("identity round trip")
	}
	for _, bad := range []string{"", "turn-1", "01900000-0000-4000-8000-000000000001"} {
		if _, err := ParseCompactionID(bad); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	for _, bad := range []string{"", " \n", string([]byte{0xff}), strings.Repeat("a", MaxContextSummaryBytes+1)} {
		if _, err := NewContextSummary(bad); err == nil {
			t.Fatal("invalid summary accepted")
		}
	}
	text := "  Error is valid summary text.\n"
	summary, err := NewContextSummary(text)
	if err != nil || summary.Text() != text {
		t.Fatal("summary altered")
	}
}

func TestActiveContextDoesNotForgeMessageIdentity(t *testing.T) {
	history := SemanticHistoryView{Provider: ProviderOpenAI, Turns: []SemanticTurn{{TurnID: "01900000-0000-7000-8000-000000000001", Messages: []SemanticMessage{{Role: SemanticUser, InputID: "01900000-0000-7000-8000-000000000002", Text: "new input"}}}}}
	summary, _ := NewContextSummary("prior work")
	view, err := NewActiveContextView(history, summary, 4)
	if err != nil {
		t.Fatal(err)
	}
	history.Turns[0].Messages[0].Text = "mutated"
	copy := view.History()
	if len(copy.Turns) != 1 || len(copy.Turns[0].Messages) != 1 || copy.Turns[0].Messages[0].Text != "new input" {
		t.Fatal("summary became a message or input alias leaked")
	}
	copy.Turns[0].Messages[0].Text = "getter mutation"
	if view.Clone().History().Turns[0].Messages[0].Text != "new input" {
		t.Fatal("getter alias leaked")
	}
	if got, ok := view.Summary(); !ok || got != summary {
		t.Fatal("summary lost")
	}
	if (ActiveContextView{}).Validate() == nil {
		t.Fatal("zero view accepted")
	}
	if _, err := NewActiveContextView(history, summary, 0); err == nil {
		t.Fatal("summary without revision accepted")
	}
}
