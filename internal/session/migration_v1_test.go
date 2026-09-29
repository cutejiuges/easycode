package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
)

type v1ReplaySummary struct {
	SessionID      domain.SessionID      `json:"session_id"`
	ThreadID       domain.ThreadID       `json:"thread_id"`
	Provider       domain.ProviderFamily `json:"provider"`
	ProviderWire   string                `json:"provider_wire"`
	Model          string                `json:"model"`
	SchemaRevision int                   `json:"schema_revision"`
	CreationCWD    string                `json:"creation_cwd"`
	Turns          []v1TurnSummary       `json:"turns"`
	NativeCommits  []v1NativeSummary     `json:"native_commits"`
	NextSequence   uint64                `json:"next_sequence"`
}

type v1TurnSummary struct {
	TurnID domain.TurnID     `json:"turn_id"`
	State  ReplayedTurnState `json:"state"`
}

type v1NativeSummary struct {
	Sequence       uint64                `json:"sequence"`
	TurnID         domain.TurnID         `json:"turn_id"`
	Provider       domain.ProviderFamily `json:"provider"`
	Wire           string                `json:"wire"`
	PayloadVersion int                   `json:"payload_version"`
}

func TestV1CompatibilityFixtureReplay(t *testing.T) {
	fixture := readV1Fixture(t, "root.jsonl")
	for _, forbidden := range []string{
		"sk-", "authorization", "cookie", "http://", "https://", "/Users/", `C:\\Users\\`,
	} {
		if strings.Contains(strings.ToLower(string(fixture)), strings.ToLower(forbidden)) {
			t.Fatalf("v1 fixture contains forbidden material %q", forbidden)
		}
	}

	repository, lease := installV1Fixture(t, fixture)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	want := readV1ReplaySummary(t)
	got := summarizeV1Replay(plan)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("v1 ReplayPlan summary = %#v, want %#v", got, want)
	}

	kinds := make(map[EventKind]bool)
	for _, record := range loaded.Records {
		kinds[record.EventKind] = true
	}
	for _, kind := range []EventKind{
		EventSessionMeta, EventThreadMeta, EventTurnStarted,
		EventProviderNativeCommit, EventTurnCompleted, EventTurnFailed,
	} {
		if !kinds[kind] {
			t.Fatalf("v1 fixture does not cover %q", kind)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestV1CompatibilityFixtureCanonicalBytesStayStable(t *testing.T) {
	fixture := bytes.TrimSuffix(readV1Fixture(t, "root.jsonl"), []byte{'\n'})
	lines := bytes.Split(fixture, []byte{'\n'})
	wantKinds := []EventKind{
		EventSessionMeta,
		EventThreadMeta,
		EventTurnStarted,
		EventTurnFailed,
		EventTurnStarted,
		EventProviderNativeCommit,
		EventTurnCompleted,
	}
	if len(lines) != len(wantKinds) {
		t.Fatalf("v1 fixture line count = %d, want %d", len(lines), len(wantKinds))
	}
	for index, line := range lines {
		record, err := DecodeRecord(line)
		if err != nil {
			t.Fatalf("decode v1 line %d: %v", index+1, err)
		}
		if record.EventKind != wantKinds[index] {
			t.Fatalf("v1 line %d kind = %q, want %q", index+1, record.EventKind, wantKinds[index])
		}
		sealed, encoded, err := EncodeRecord(record)
		if err != nil {
			t.Fatalf("encode v1 line %d: %v", index+1, err)
		}
		if !bytes.Equal(encoded, line) || sealed.Checksum != record.Checksum {
			t.Fatalf("v1 line %d canonical bytes or checksum changed\n got: %s\nwant: %s", index+1, encoded, line)
		}
	}
}

func TestV1CompatibilityFixtureContinuesWithoutRewrite(t *testing.T) {
	prefix := readV1Fixture(t, "root.jsonl")
	repository, lease := installV1Fixture(t, prefix)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		t.Fatal(err)
	}
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{
		mustDraft(t, EventTurnStarted, domain.TurnID("00000000-0007-7000-8000-000000000008"), TurnStartedPayload{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Sequence != 8 {
		t.Fatalf("continued records = %#v", records)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) <= len(prefix) || !bytes.Equal(content[:len(prefix)], prefix) {
		t.Fatal("continuing the v1 fixture rewrote its historical prefix")
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestV1CompatibilityFixtureRejectsNewerRequiredReadOnly(t *testing.T) {
	baseline := readV1Fixture(t, "root.jsonl")
	newerRequired := readV1Fixture(t, "newer_required.jsonl")
	fixtures := map[string][]byte{
		"schema revision":  bytes.Replace(baseline, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1),
		"payload revision": append(append([]byte(nil), baseline...), newerRequired...),
	}
	for name, content := range fixtures {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			repository, lease := installV1Fixture(t, content)
			loaded, loadErr := NewLoader().Load(context.Background(), lease)
			if loadErr == nil {
				_, loadErr = NewReplayPlanner().Plan(loaded)
			}
			if loadErr == nil || (!strings.Contains(loadErr.Error(), "unsupported") &&
				!strings.Contains(loadErr.Error(), "required")) {
				t.Fatalf("newer required fixture error = %v", loadErr)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			path, err := repository.JournalPath(testThreadID)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, content) {
				t.Fatal("rejecting a newer required revision modified journal bytes")
			}
			if err := repository.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func installV1Fixture(t *testing.T, content []byte) (*Repository, *JournalLease) {
	t.Helper()
	repository, err := OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Create(context.Background(), testThreadID)
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	lease, err = repository.Open(context.Background(), testThreadID)
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	return repository, lease
}

func readV1Fixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "migrations", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func readV1ReplaySummary(t *testing.T) v1ReplaySummary {
	t.Helper()
	content := readV1Fixture(t, "replay_plan.json")
	var summary v1ReplaySummary
	if err := json.Unmarshal(content, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func summarizeV1Replay(plan ReplayPlan) v1ReplaySummary {
	summary := v1ReplaySummary{
		SessionID: plan.Identity.SessionID, ThreadID: plan.Identity.ThreadID,
		Provider: plan.SessionMetadata.Provider, ProviderWire: plan.SessionMetadata.ProviderWire,
		Model: plan.SessionMetadata.Model, SchemaRevision: plan.SessionMetadata.SchemaRevision,
		CreationCWD: plan.SessionMetadata.CreationCWD, NextSequence: plan.NextSequence,
		Turns:         make([]v1TurnSummary, 0, len(plan.Turns)),
		NativeCommits: make([]v1NativeSummary, 0, len(plan.NativeCommits)),
	}
	for _, turn := range plan.Turns {
		summary.Turns = append(summary.Turns, v1TurnSummary(turn))
	}
	for _, commit := range plan.NativeCommits {
		summary.NativeCommits = append(summary.NativeCommits, v1NativeSummary{
			Sequence: commit.Sequence, TurnID: commit.TurnID, Provider: commit.Commit.Provider,
			Wire: commit.Commit.Wire, PayloadVersion: commit.Commit.PayloadVersion,
		})
	}
	return summary
}
