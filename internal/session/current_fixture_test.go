package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestCurrentFixtureCanonicalBytesStayStable(t *testing.T) {
	fixture := bytes.TrimSuffix(readCurrentFixture(t, "root.jsonl"), []byte{'\n'})
	lines := bytes.Split(fixture, []byte{'\n'})
	wantKinds := []EventKind{
		EventSessionMeta,
		EventThreadMeta,
		EventTurnStarted,
		EventTurnFailed,
		EventTurnStarted,
		EventTurnInputsCommitted, EventProviderNativeCommit,
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

func TestCurrentFixtureContinuesWithoutRewrite(t *testing.T) {
	prefix := readCurrentFixture(t, "root.jsonl")
	repository, lease := installCurrentFixture(t, prefix)
	loaded := loadFixtureJournal(t, context.Background(), lease)
	writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	requireNoError(t, err)
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{
		mustDraft(t, EventTurnStarted, domain.TurnID("00000000-0007-7000-8000-000000000008"), TurnStartedPayload{}),
	})
	requireNoError(t, err)
	if len(records) != 1 || records[0].Sequence != 11 {
		t.Fatalf("continued records = %#v", records)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	requireNoError(t, err)
	content := readSessionFixtureFile(t, path)
	if len(content) <= len(prefix) || !bytes.Equal(content[:len(prefix)], prefix) {
		t.Fatal("continuing the current fixture rewrote its historical prefix")
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentFixtureRejectsUnsupportedShapeReadOnly(t *testing.T) {
	baseline := readCurrentFixture(t, "root.jsonl")
	invalidFields := readCurrentFixture(t, "invalid_version_fields.jsonl")
	fixtures := map[string][]byte{
		"unexpected version":     append([]byte(`{"schema_version":1,`), baseline[1:]...),
		"version fields in tail": append(append([]byte(nil), baseline...), invalidFields...),
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
				!strings.Contains(loadErr.Error(), "unknown")) {
				t.Fatalf("unsupported fixture error = %v", loadErr)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			assertJournalBytes(t, repository, content)
			if err := repository.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func installCurrentFixture(t *testing.T, content []byte) (*Repository, *JournalLease) {
	t.Helper()
	repository := openFixtureRepository(t, filepath.Join(t.TempDir(), "sessions"))
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
	content := readSessionFixtureFile(t, filepath.Join("testdata", "current", name))
	return content
}

func openFixtureRepository(t *testing.T, root string) *Repository {
	t.Helper()
	repository, err := OpenOrCreateRepository(root)
	requireNoError(t, err)
	return repository
}

func loadFixtureJournal(t *testing.T, ctx context.Context, lease *JournalLease) LoadResult {
	t.Helper()
	loaded, err := NewLoader().Load(ctx, lease)
	requireNoError(t, err)
	return loaded
}

func readSessionFixtureFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	requireNoError(t, err)
	return content
}

func decodeFixtureRecord(t *testing.T, content []byte) Record {
	t.Helper()
	record, err := DecodeRecord(content)
	requireNoError(t, err)
	return record
}

func requireNoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
