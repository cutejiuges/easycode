package catalog

import (
	"encoding/json"
	"path"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestProjectorUsesCommittedTail(t *testing.T) {
	identity := catalogTestIdentity(t)
	createdAt := time.Unix(100, 0).UTC()
	updatedAt := time.Unix(200, 0).UTC()
	records := []session.Record{
		{Sequence: 1, Timestamp: createdAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "first"},
		{Sequence: 2, Timestamp: updatedAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "last"},
	}
	loaded := session.LoadResult{Identity: identity, Records: records, NextSequence: 3}
	plan := session.ReplayPlan{
		Identity: identity,
		SessionMetadata: session.SessionMetaPayload{
			RootThreadID: identity.ThreadID, CreatedAt: createdAt,
			Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
			CreationCWD: t.TempDir(),
		},
		ThreadMetadata: session.ThreadMetaPayload{Root: true}, NextSequence: 3,
	}
	relative := catalogTestJournalPath(t, identity.ThreadID)
	entry, err := NewProjector().Project(relative, loaded, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.UpdatedAt.Equal(updatedAt) || entry.LastSequence != 2 || entry.LastChecksum != "last" {
		t.Fatalf("entry tail = %#v", entry)
	}
}

func TestProjectorRejectsInvalidInputs(t *testing.T) {
	identity := catalogTestIdentity(t)
	now := time.Unix(200, 0).UTC()
	record := session.Record{Sequence: 1, Timestamp: now, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "checksum"}
	loaded := session.LoadResult{Identity: identity, Records: []session.Record{record}, NextSequence: 2}
	base := session.ReplayPlan{
		Identity: identity,
		SessionMetadata: session.SessionMetaPayload{
			RootThreadID: identity.ThreadID, CreatedAt: time.Unix(100, 0).UTC(),
			Provider: domain.ProviderAnthropic, ProviderWire: "messages", Model: "claude-test",
			CreationCWD: t.TempDir(),
		},
		ThreadMetadata: session.ThreadMetaPayload{Root: true}, NextSequence: 2,
	}
	tests := []struct {
		name     string
		relative string
		loaded   session.LoadResult
		plan     session.ReplayPlan
	}{
		{name: "relative path escape", relative: "../outside.jsonl", loaded: loaded, plan: base},
		{name: "non root", relative: catalogTestJournalPath(t, identity.ThreadID), loaded: loaded, plan: func() session.ReplayPlan { value := base; value.ThreadMetadata.Root = false; return value }()},
		{name: "zero time", relative: catalogTestJournalPath(t, identity.ThreadID), loaded: loaded, plan: func() session.ReplayPlan { value := base; value.SessionMetadata.CreatedAt = time.Time{}; return value }()},
		{name: "identity mismatch", relative: catalogTestJournalPath(t, identity.ThreadID), loaded: func() session.LoadResult { value := loaded; value.Identity = session.Identity{}; return value }(), plan: base},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewProjector().Project(test.relative, test.loaded, test.plan); err == nil {
				t.Fatal("Project() unexpectedly succeeded")
			}
		})
	}
}

func TestProjectorPreservesFailedAndInterruptedTailsForBothProviders(t *testing.T) {
	for _, test := range []struct {
		name        string
		family      domain.ProviderFamily
		wire        string
		interrupted bool
	}{
		{name: "OpenAI failed", family: domain.ProviderOpenAI, wire: "responses"},
		{name: "Anthropic interrupted", family: domain.ProviderAnthropic, wire: "messages", interrupted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := catalogTestIdentity(t)
			createdAt := time.Unix(100, 0).UTC()
			updatedAt := time.Unix(300, 0).UTC()
			records := []session.Record{
				{Sequence: 1, Timestamp: createdAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "first"},
				{Sequence: 2, Timestamp: createdAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "metadata"},
				{Sequence: 3, Timestamp: updatedAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "tail"},
			}
			loaded := session.LoadResult{Identity: identity, Records: records, NextSequence: 4}
			plan := session.ReplayPlan{
				Identity: identity,
				SessionMetadata: session.SessionMetaPayload{
					RootThreadID: identity.ThreadID, CreatedAt: createdAt,
					Provider: test.family, ProviderWire: test.wire, Model: "model-test", CreationCWD: t.TempDir(),
				},
				ThreadMetadata: session.ThreadMetaPayload{Root: true}, NextSequence: 4,
			}
			if test.interrupted {
				plan.InterruptedTail = &session.InterruptedTail{Sequence: 3}
			} else {
				plan.Turns = []session.ReplayedTurn{{State: session.ReplayedTurnFailed}}
			}
			entry, err := NewProjector().Project(catalogTestJournalPath(t, identity.ThreadID), loaded, plan)
			if err != nil {
				t.Fatal(err)
			}
			if entry.ProviderFamily != test.family || entry.ProviderWire != test.wire ||
				entry.LastSequence != 3 || !entry.UpdatedAt.Equal(updatedAt) {
				t.Fatalf("entry = %#v", entry)
			}
		})
	}
}

func TestProjectorUsesToolTailRecencyWithoutCopyingLedgerDetails(t *testing.T) {
	identity := catalogTestIdentity(t)
	turnID, err := domain.GenerateTurnID()
	if err != nil {
		t.Fatal(err)
	}
	invocationID, err := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("sensitive-call-id")
	input, _ := tool.NewReadInput("private/source.go", 1, 20)
	ready, _ := tool.NewReadReadyCall(callID, input)
	invocation, err := tool.NewInvocation(invocationID, ready)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := session.NewToolRecoveryPlan(turnID, 1, []session.ReplayedToolCall{{
		ReadySequence: 3, SampleIndex: 0, CallIndex: 0,
		Invocation: invocation, State: session.ReplayedToolCallReady,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Unix(100, 0).UTC()
	updatedAt := time.Unix(300, 0).UTC()
	loaded := session.LoadResult{
		Identity: identity,
		Records: []session.Record{
			{Sequence: 1, Timestamp: createdAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "first"},
			{Sequence: 2, Timestamp: createdAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID, Checksum: "metadata"},
			{
				Sequence: 3, Timestamp: updatedAt, SessionID: identity.SessionID, ThreadID: identity.ThreadID,
				EventKind: session.EventToolCallReady, Checksum: "tool-tail",
				Payload: []byte(`{"file_path":"private/source.go","call_id":"sensitive-call-id"}`),
			},
		},
		NextSequence: 4,
	}
	plan := session.ReplayPlan{
		Identity: identity,
		SessionMetadata: session.SessionMetaPayload{
			RootThreadID: identity.ThreadID, CreatedAt: createdAt,
			Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
			CreationCWD: t.TempDir(),
		},
		ThreadMetadata: session.ThreadMetaPayload{Root: true},
		ToolRecovery:   &recovery,
		NextSequence:   4,
	}
	entry, err := NewProjector().Project(catalogTestJournalPath(t, identity.ThreadID), loaded, plan)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if entry.LastSequence != 3 || entry.LastChecksum != "tool-tail" || !entry.UpdatedAt.Equal(updatedAt) ||
		strings.Contains(string(encoded), "private/source.go") || strings.Contains(string(encoded), "sensitive-call-id") {
		t.Fatalf("tool tail catalog entry = %s", encoded)
	}
}

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
