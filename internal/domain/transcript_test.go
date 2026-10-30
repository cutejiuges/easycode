package domain

import (
	"easycode/internal/codec"
	"strings"
	"testing"
)

func TestTranscriptClosedKindsAndCopy(t *testing.T) {
	turn := TurnID("01900000-0000-7000-8000-000000000001")
	user, err := NewTranscriptUser(TranscriptPosition{}, turn, InputID(turn), "  中文\n")
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := NewTranscriptAssistant(TranscriptPosition{Entry: 1}, turn, "before\n")
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewTranscriptCall(TranscriptPosition{Entry: 1, Item: 1}, turn, "call-1", "Read")
	if err != nil {
		t.Fatal(err)
	}
	items := []TranscriptItem{user, assistant, call}
	view, err := NewTranscriptView(ProviderOpenAI, items)
	if err != nil {
		t.Fatal(err)
	}
	items[0].Text = "changed"
	if view.Items[0].Text != "  中文\n" {
		t.Fatal("shared input slice")
	}
	for _, item := range view.Items {
		data, err := codec.MarshalStable(item)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeTranscriptItem(data)
		if err != nil || decoded != item {
			t.Fatalf("round trip: %v %#v", err, decoded)
		}
		for _, invalid := range []string{
			strings.Replace(string(data), `"kind":`, `"unknown":true,"kind":`, 1),
			strings.Replace(string(data), `"text":`, `"thinking":"private","text":`, 1),
			strings.Replace(string(data), `"entry":0`, `"entry":null`, 1),
		} {
			if invalid == string(data) {
				continue
			}
			if _, err := DecodeTranscriptItem([]byte(invalid)); err == nil {
				t.Fatalf("accepted invalid: %s", invalid)
			}
		}
	}
	for _, mutate := range []func(*TranscriptItem){
		func(v *TranscriptItem) { v.Kind = "unknown" },
		func(v *TranscriptItem) { v.InputID = InputID(turn) },
		func(v *TranscriptItem) { v.Text = "arguments" },
		func(v *TranscriptItem) { v.CallID = "" },
		func(v *TranscriptItem) { v.CallName = strings.Repeat("x", 513) },
	} {
		bad := call
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("accepted invalid call")
		}
	}
	if _, err := NewTranscriptView(ProviderOpenAI, []TranscriptItem{call, assistant}); err == nil {
		t.Fatal("accepted reversed order")
	}
	duplicate := user
	duplicate.Position.Entry = 2
	if _, err := NewTranscriptView(ProviderOpenAI, []TranscriptItem{user, duplicate}); err == nil {
		t.Fatal("accepted duplicate input")
	}
}
