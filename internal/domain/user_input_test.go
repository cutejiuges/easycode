package domain

import (
	"strings"
	"testing"
)

func TestUserInputPreservesTextAndValidatesIdentity(t *testing.T) {
	id := InputID("01900000-0000-7000-8000-000000000001")
	text := "  追加\n正文 \n"
	input, err := NewUserInput(id, text)
	if err != nil || input.ID() != id || input.Text() != text {
		t.Fatalf("input did not preserve text: %v", err)
	}
	for _, text := range []string{"", " \n\t", string([]byte{0xff}), strings.Repeat("x", MaxUserInputBytes+1)} {
		if _, err := NewUserInput(id, text); err == nil {
			t.Fatal("invalid text was accepted")
		}
	}
	if _, err := NewUserInput("not-an-id", "text"); err == nil {
		t.Fatal("invalid identity was accepted")
	}
	if _, err := NewUserInput(id, strings.Repeat("x", MaxUserInputBytes)); err != nil {
		t.Fatal(err)
	}
}

func TestInputAssociationIsOrderedIndependentAndClosed(t *testing.T) {
	turnID := TurnID("01900000-0000-7000-8000-000000000001")
	ids := []InputID{"01900000-0000-7000-8000-000000000002", "01900000-0000-7000-8000-000000000003"}
	association, err := NewInputAssociation(turnID, InputSteering, 1, ids)
	if err != nil {
		t.Fatal(err)
	}
	first := ids[0]
	ids[0] = ids[1]
	copy := association.IDs()
	copy[0] = copy[1]
	if association.IDs()[0] != first || association.TurnID() != turnID || association.SampleIndex() != 1 || association.Placement() != InputSteering {
		t.Fatal("association identity or order changed")
	}
	for _, test := range []struct {
		placement InputPlacement
		index     uint32
		ids       []InputID
	}{
		{InputInitial, 1, association.IDs()},
		{InputSteering, 0, association.IDs()},
		{"future", 0, association.IDs()},
		{InputInitial, 0, nil},
		{InputInitial, 0, []InputID{first, first}},
		{InputInitial, 0, []InputID{"invalid"}},
	} {
		if _, err := NewInputAssociation(turnID, test.placement, test.index, test.ids); err == nil {
			t.Fatal("invalid association accepted")
		}
	}
	if (InputAssociation{}).Validate() == nil || (UserInput{}).Validate() == nil {
		t.Fatal("zero value accepted")
	}
}
