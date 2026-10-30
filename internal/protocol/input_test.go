package protocol

import (
	"easycode/internal/domain"
	"testing"
)

func TestInputStatusClosedCombinations(t *testing.T) {
	id := domain.InputID("00000000-0013-7000-8000-000000000013")
	for _, state := range []InputState{InputClaimed, InputCommitted, InputReturned, InputUncertain, "future", ""} {
		for _, reason := range []InputReason{"", InputReasonRetracted, InputReasonStopped, InputReasonCancelled, InputReasonShutdown, InputReasonFailed, InputReasonPreflight, InputReasonUncertain, "future"} {
			for _, turn := range []domain.TurnID{"", testTurnID} {
				want := ((state == InputClaimed || state == InputCommitted) && reason == "" && turn.Valid()) || (state == InputUncertain && reason == InputReasonUncertain && turn.Valid()) || (state == InputReturned && reason != "" && reason != InputReasonUncertain && reason != "future")
				status, err := NewInputStatus(testRequestID, id, state, turn, reason)
				if (err == nil) != want {
					t.Fatalf("state=%s reason=%s turn=%s: %v", state, reason, turn, err)
				}
				if want {
					item, err := NewInputStatusItem(status)
					if err != nil {
						t.Fatal(err)
					}
					got, ok := item.InputStatus()
					if !ok || got != status {
						t.Fatal("status changed")
					}
				}
			}
		}
	}
	if (InputStatus{}).Validate() == nil {
		t.Fatal("zero status accepted")
	}
}

func TestUserInputCommittedStrictDecoder(t *testing.T) {
	id := domain.InputID("00000000-0013-7000-8000-000000000013")
	input, _ := domain.NewUserInput(id, " 你好\n ")
	event, err := NewUserInputCommitted(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeUserInputCommitted(event)
	if err != nil || got.InputID != id || got.Text != input.Text() {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, payload := range []string{`{}`, `null`, `{"input_id":null,"text":"x"}`, `{"input_id":"00000000-0013-7000-8000-000000000013","text":"x","extra":true}`, `{"input_id":"00000000-0013-7000-8000-000000000013","text":"x","text":"y"}`} {
		event.Payload = []byte(payload)
		if _, err := DecodeUserInputCommitted(event); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}
