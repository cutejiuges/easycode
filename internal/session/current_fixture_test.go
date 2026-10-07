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

type currentReplaySummary struct {
	SessionID      domain.SessionID       `json:"session_id"`
	ThreadID       domain.ThreadID        `json:"thread_id"`
	Provider       domain.ProviderFamily  `json:"provider"`
	ProviderWire   string                 `json:"provider_wire"`
	Model          string                 `json:"model"`
	SchemaRevision int                    `json:"schema_revision"`
	CreationCWD    string                 `json:"creation_cwd"`
	Turns          []currentTurnSummary   `json:"turns"`
	NativeCommits  []currentNativeSummary `json:"native_commits"`
	SampleUsages   []currentUsageSummary  `json:"sample_usages"`
	NextSequence   uint64                 `json:"next_sequence"`
}

type currentTurnSummary struct {
	TurnID domain.TurnID     `json:"turn_id"`
	State  ReplayedTurnState `json:"state"`
}

type currentNativeSummary struct {
	Sequence       uint64                `json:"sequence"`
	TurnID         domain.TurnID         `json:"turn_id"`
	Provider       domain.ProviderFamily `json:"provider"`
	Wire           string                `json:"wire"`
	PayloadVersion int                   `json:"payload_version"`
}

type currentUsageSummary struct {
	Sequence uint64             `json:"sequence"`
	TurnID   domain.TurnID      `json:"turn_id"`
	Usage    SampleUsagePayload `json:"usage"`
}

func TestCurrentFixtureReplay(t *testing.T) {
	fixture := readCurrentFixture(t, "root.jsonl")
	for _, forbidden := range []string{
		"sk-", "authorization", "cookie", "http://", "https://", "/Users/", `C:\\Users\\`,
	} {
		if strings.Contains(strings.ToLower(string(fixture)), strings.ToLower(forbidden)) {
			t.Fatalf("current fixture contains forbidden material %q", forbidden)
		}
	}

	repository, lease := installCurrentFixture(t, fixture)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	want := readCurrentReplaySummary(t)
	got := summarizeCurrentReplay(t, plan)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current ReplayPlan summary = %#v, want %#v", got, want)
	}

	kinds := make(map[EventKind]bool)
	for _, record := range loaded.Records {
		kinds[record.EventKind] = true
	}
	for _, kind := range []EventKind{
		EventSessionMeta, EventThreadMeta, EventTurnStarted,
		EventProviderNativeCommit, EventSampleUsage, EventTurnCompleted, EventTurnFailed,
	} {
		if !kinds[kind] {
			t.Fatalf("current fixture does not cover %q", kind)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentFixtureCanonicalBytesStayStable(t *testing.T) {
	fixture := bytes.TrimSuffix(readCurrentFixture(t, "root.jsonl"), []byte{'\n'})
	lines := bytes.Split(fixture, []byte{'\n'})
	wantKinds := []EventKind{
		EventSessionMeta,
		EventThreadMeta,
		EventTurnStarted,
		EventTurnFailed,
		EventTurnStarted,
		EventProviderNativeCommit,
		EventSampleUsage,
		EventTurnCompleted,
	}
	if len(lines) != len(wantKinds) {
		t.Fatalf("current fixture line count = %d, want %d", len(lines), len(wantKinds))
	}
	for index, line := range lines {
		record, err := DecodeRecord(line)
		if err != nil {
			t.Fatalf("decode current line %d: %v", index+1, err)
		}
		if record.EventKind != wantKinds[index] {
			t.Fatalf("current line %d kind = %q, want %q", index+1, record.EventKind, wantKinds[index])
		}
		sealed, encoded, err := EncodeRecord(record)
		if err != nil {
			t.Fatalf("encode current line %d: %v", index+1, err)
		}
		if !bytes.Equal(encoded, line) || sealed.Checksum != record.Checksum {
			t.Fatalf("current line %d canonical bytes or checksum changed\n got: %s\nwant: %s", index+1, encoded, line)
		}
	}
}

func TestCurrentToolLoopFixtureReplay(t *testing.T) {
	fixture := bytes.TrimSuffix(readCurrentFixture(t, "tool_loop.jsonl"), []byte{'\n'})
	wantKinds := []EventKind{
		EventSessionMeta, EventThreadMeta, EventTurnStarted,
		EventProviderNativeCommit, EventSampleUsage, EventToolCallReady,
		EventToolExecutionStarted, EventToolCallResult,
		EventProviderNativeCommit, EventTurnFailed,
	}
	lines := bytes.Split(fixture, []byte{'\n'})
	if len(lines) != len(wantKinds) {
		t.Fatalf("tool loop fixture line count = %d, want %d", len(lines), len(wantKinds))
	}
	for index, line := range lines {
		record, err := DecodeRecord(line)
		if err != nil {
			t.Fatalf("decode tool loop line %d: %v", index+1, err)
		}
		if record.EventKind != wantKinds[index] {
			t.Fatalf("tool loop line %d kind = %q, want %q", index+1, record.EventKind, wantKinds[index])
		}
		_, encoded, err := EncodeRecord(record)
		if err != nil {
			t.Fatalf("encode tool loop line %d: %v", index+1, err)
		}
		if !bytes.Equal(encoded, line) {
			t.Fatalf("tool loop line %d canonical bytes changed", index+1)
		}
	}

	repository, lease := installCurrentFixture(t, append(fixture, '\n'))
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ToolRecovery != nil || plan.InterruptedTail != nil {
		t.Fatalf("closed tool loop retained recovery state: %#v", plan)
	}
	var want currentReplaySummary
	if err := json.Unmarshal(readCurrentFixture(t, "tool_loop_replay_plan.json"), &want); err != nil {
		t.Fatal(err)
	}
	if got := summarizeCurrentReplay(t, plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("tool loop ReplayPlan summary = %#v, want %#v", got, want)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentFixtureContinuesWithoutRewrite(t *testing.T) {
	prefix := readCurrentFixture(t, "root.jsonl")
	repository, lease := installCurrentFixture(t, prefix)
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
	if len(records) != 1 || records[0].Sequence != 9 {
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
		t.Fatal("continuing the current fixture rewrote its historical prefix")
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentCompletionWithoutSampleUsageFailsClosed(t *testing.T) {
	fixture := readCurrentFixture(t, "rejected_without_usage.jsonl")
	repository, lease := installCurrentFixture(t, fixture)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReplayPlanner().Plan(loaded); err == nil || !strings.Contains(err.Error(), "sample batch") {
		t.Fatalf("current fixture replay error = %v", err)
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
	if !bytes.Equal(after, fixture) {
		t.Fatal("rejecting completion without usage modified journal bytes")
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentFixtureRejectsNewerRequiredReadOnly(t *testing.T) {
	baseline := readCurrentFixture(t, "root.jsonl")
	newerRequired := readCurrentFixture(t, "newer_required.jsonl")
	fixtures := map[string][]byte{
		"schema revision":  bytes.Replace(baseline, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1),
		"payload revision": append(append([]byte(nil), baseline...), newerRequired...),
	}
	for name, content := range fixtures {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			repository, lease := installCurrentFixture(t, content)
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

func installCurrentFixture(t *testing.T, content []byte) (*Repository, *JournalLease) {
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

func readCurrentFixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "current", name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func readCurrentReplaySummary(t *testing.T) currentReplaySummary {
	t.Helper()
	content := readCurrentFixture(t, "replay_plan.json")
	var summary currentReplaySummary
	if err := json.Unmarshal(content, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func summarizeCurrentReplay(t *testing.T, plan ReplayPlan) currentReplaySummary {
	t.Helper()
	summary := currentReplaySummary{
		SessionID: plan.Identity.SessionID, ThreadID: plan.Identity.ThreadID,
		Provider: plan.SessionMetadata.Provider, ProviderWire: plan.SessionMetadata.ProviderWire,
		Model: plan.SessionMetadata.Model, SchemaRevision: plan.SessionMetadata.SchemaRevision,
		CreationCWD: plan.SessionMetadata.CreationCWD, NextSequence: plan.NextSequence,
		Turns:         make([]currentTurnSummary, 0, len(plan.Turns)),
		NativeCommits: make([]currentNativeSummary, 0, len(plan.NativeCommits)),
		SampleUsages:  make([]currentUsageSummary, 0, len(plan.SampleUsages)),
	}
	for _, turn := range plan.Turns {
		summary.Turns = append(summary.Turns, currentTurnSummary(turn))
	}
	for _, commit := range plan.NativeCommits {
		summary.NativeCommits = append(summary.NativeCommits, currentNativeSummary{
			Sequence: commit.Sequence, TurnID: commit.TurnID, Provider: commit.Commit.Provider,
			Wire: commit.Commit.Wire, PayloadVersion: commit.Commit.PayloadVersion,
		})
	}
	for _, usage := range plan.SampleUsages {
		payload, err := NewSampleUsagePayload(usage.Usage)
		if err != nil {
			t.Fatal(err)
		}
		summary.SampleUsages = append(summary.SampleUsages, currentUsageSummary{
			Sequence: usage.Sequence, TurnID: usage.TurnID, Usage: payload,
		})
	}
	return summary
}
