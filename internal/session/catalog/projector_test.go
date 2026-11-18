package catalog

import (
	"path"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func catalogTestIdentity(t *testing.T) session.Identity {
	t.Helper()
	sessionID, err := domain.GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.GenerateThreadID()
	if err != nil {
		t.Fatal(err)
	}
	return session.Identity{SessionID: sessionID, ThreadID: threadID}
}

func catalogTestJournalPath(t *testing.T, threadID domain.ThreadID) string {
	t.Helper()
	timestamp, err := threadID.Time()
	if err != nil {
		t.Fatal(err)
	}
	return path.Join(timestamp.Format("2006"), timestamp.Format("01"), timestamp.Format("02"), string(threadID)+".jsonl")
}
