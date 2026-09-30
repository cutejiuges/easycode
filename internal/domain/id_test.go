package domain

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestUUIDv7RoundTripAcrossDates(t *testing.T) {
	t.Parallel()
	fixtures := []time.Time{
		time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.September, 28, 23, 59, 59, 999_000_000, time.FixedZone("fixture", 8*60*60)),
		time.UnixMilli(maxUUIDv7UnixMilli).UTC(),
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.String(), func(t *testing.T) {
			random := bytes.NewReader(bytes.Repeat([]byte{0xab}, 10))
			value, err := generateUUIDv7(fixture, random)
			if err != nil {
				t.Fatalf("generateUUIDv7() error = %v", err)
			}
			threadID, err := ParseThreadID(value)
			if err != nil {
				t.Fatalf("ParseThreadID() error = %v", err)
			}
			resolved, err := threadID.Time()
			if err != nil {
				t.Fatalf("Time() error = %v", err)
			}
			want := time.UnixMilli(fixture.UnixMilli()).UTC()
			if !resolved.Equal(want) {
				t.Fatalf("Time() = %v, want %v", resolved, want)
			}
			if value[14] != '7' || value[19] != 'a' {
				t.Fatalf("UUIDv7 version/variant = %q", value)
			}
		})
	}
}

func TestUUIDv7RejectsInvalidCanonicalForms(t *testing.T) {
	t.Parallel()
	valid, err := generateUUIDv7(time.UnixMilli(1), bytes.NewReader(make([]byte, 10)))
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []string{
		"",
		"../" + valid,
		"00000000-0000-4000-8000-000000000000",
		"00000000-0000-7000-0000-000000000000",
		"00000000-0000-7000-8000-00000000000G",
		"00000000-0000-7000-8000-00000000000A",
		"00000000000070008000000000000000",
	}
	for _, fixture := range fixtures {
		if _, err := ParseSessionID(fixture); err == nil {
			t.Errorf("ParseSessionID(%q) unexpectedly succeeded", fixture)
		}
		if SessionID(fixture).Valid() {
			t.Errorf("SessionID(%q).Valid() = true", fixture)
		}
	}
}

func TestUUIDv7RejectsAbnormalGenerationInputs(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name   string
		now    time.Time
		random *bytes.Reader
	}{
		{name: "before epoch", now: time.UnixMilli(-1), random: bytes.NewReader(make([]byte, 10))},
		{name: "after range", now: time.UnixMilli(maxUUIDv7UnixMilli).Add(time.Millisecond), random: bytes.NewReader(make([]byte, 10))},
		{name: "short randomness", now: time.UnixMilli(1), random: bytes.NewReader(make([]byte, 9))},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := generateUUIDv7(fixture.now, fixture.random); err == nil {
				t.Fatal("generateUUIDv7() unexpectedly succeeded")
			}
		})
	}
	if _, err := generateUUIDv7(time.UnixMilli(1), nil); err == nil {
		t.Fatal("generateUUIDv7() with nil random source unexpectedly succeeded")
	}
}

func TestPublicUUIDv7Generators(t *testing.T) {
	t.Parallel()
	sessionID, err := GenerateSessionID()
	if err != nil || !sessionID.Valid() {
		t.Fatalf("GenerateSessionID() = %q, %v", sessionID, err)
	}
	threadID, err := GenerateThreadID()
	if err != nil || !threadID.Valid() {
		t.Fatalf("GenerateThreadID() = %q, %v", threadID, err)
	}
	turnID, err := GenerateTurnID()
	if err != nil || !turnID.Valid() {
		t.Fatalf("GenerateTurnID() = %q, %v", turnID, err)
	}
	if _, err := SessionID("invalid").Time(); err == nil {
		t.Fatal("invalid SessionID.Time() unexpectedly succeeded")
	}
	if _, err := ThreadID("invalid").Time(); err == nil {
		t.Fatal("invalid ThreadID.Time() unexpectedly succeeded")
	}
	if _, err := TurnID("invalid").Time(); err == nil {
		t.Fatal("invalid TurnID.Time() unexpectedly succeeded")
	}
	if _, err := generateUUIDv7(time.Now(), errorReader{}); err == nil {
		t.Fatal("generateUUIDv7() with failing source unexpectedly succeeded")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture error")
}
