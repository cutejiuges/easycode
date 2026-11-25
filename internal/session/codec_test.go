package session

import (
	"testing"
	"time"

	"easycode/internal/domain"
)

const (
	testSessionID = domain.SessionID("00000000-0000-7000-8000-000000000001")
	testThreadID  = domain.ThreadID("00000000-0001-7000-8000-000000000002")
	testTurnID    = domain.TurnID("00000000-0002-7000-8000-000000000003")
)

func mustRecord(t *testing.T, kind EventKind, payload any) []byte {
	t.Helper()
	record, err := BuildRecord(
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		mustDraft(t, kind, testTurnID, payload),
		1, time.Unix(0, 0).UTC(), 1, 0, 1,
	)
	requireNoError(t, err)
	_, encoded, err := EncodeRecord(record)
	requireNoError(t, err)
	return encoded
}

func mustDecodedRecord(t *testing.T, kind EventKind, payload any) Record {
	t.Helper()
	encoded := mustRecord(t, kind, payload)
	record := decodeFixtureRecord(t, encoded)
	return record
}
