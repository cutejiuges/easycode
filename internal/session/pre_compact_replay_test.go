package session

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestPreCompactReplayKeepsStopRecoveryAtEachBoundary(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"stop_compaction_started.jsonl", "stop_compaction_failed.jsonl"} {
		t.Run(name, func(t *testing.T) {
			content := readCurrentFixture(t, name)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(ctx, lease)
			if err != nil || loaded.NeedsRepair() {
				t.Fatal("invalid current fixture", err)
			}
			planner := NewReplayPlanner()
			for i, record := range loaded.Records {
				switch record.EventKind {
				case EventCompactionStarted, EventPreCompactHookStarted, EventPreCompactHookFinished, EventPreCompactHookDecided:
					prefix := finalLoadResult(loaded.Identity, loaded.Records[:i+1], testThreadID, RepairReport{})
					plan, err := planner.Plan(prefix)
					if err != nil || plan.InterruptedCompaction == nil || plan.StopRecovery == nil || !plan.StopRecovery.State().PendingFeedback() || len(plan.NativeCommits) != 2 {
						t.Fatalf("%s did not preserve the pending Stop boundary: %v", record.EventKind, err)
					}
				}
				if _, encoded, err := EncodeRecord(record); err != nil || !bytes.Contains(content, append(encoded, '\n')) {
					t.Fatalf("current fixture record is not canonical: %v", err)
				}
			}
			plan, err := planner.Plan(loaded)
			requireNoError(t, err)
			if name == "stop_compaction_failed.jsonl" {
				if plan.InterruptedCompaction != nil || len(plan.Compactions) != 1 || plan.Compactions[0].Failed == nil || plan.Compactions[0].Failed.Code != protocol.CompactionFailureHookDenied || plan.StopRecovery == nil || !plan.StopRecovery.State().PendingFeedback() {
					t.Fatal("the hook denial did not close only the compaction")
				}
				return
			}
			if plan.InterruptedCompaction == nil {
				t.Fatal("missing interrupted compaction")
			}
		})
	}
}

func TestPreCompactReplayRejectsForgedEvidenceBeforeTornTailRepair(t *testing.T) {
	tests := []struct {
		name string
		kind EventKind
		edit func(*testing.T, *Record)
	}{
		{"missing discriminator", EventCompactionStarted, func(t *testing.T, record *Record) {
			var payload map[string]json.RawMessage
			if err := codec.Unmarshal(record.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			delete(payload, "pre_compact")
			record.Payload, _ = codec.MarshalStable(payload)
		}},
		{"foreign trigger despite valid plan digest", EventCompactionStarted, func(t *testing.T, record *Record) {
			start, err := DecodeCompactionStartedPayload(*record)
			requireNoError(t, err)
			start.PreCompact.checks[0].Matcher = "manual"
			start.PreCompact.digest, err = start.PreCompact.computeDigest(start.ID, start.Scope, start.Source)
			requireNoError(t, err)
			record.Payload, _ = codec.MarshalStable(start)
		}},
		{"singleton with forged batch width", EventPreCompactHookStarted, func(_ *testing.T, record *Record) {
			record.BatchSize = 2
		}},
		{"forged exit two block", EventPreCompactHookFinished, func(_ *testing.T, record *Record) {
			record.Payload = bytes.Replace(record.Payload, []byte(`"exit_code":0`), []byte(`"exit_code":2`), 1)
		}},
		{"terminal cannot change the block reason", EventCompactionFailed, func(_ *testing.T, record *Record) {
			record.Payload = bytes.Replace(record.Payload, []byte(`"code":"hook_denied"`), []byte(`"code":"hook_failed"`), 1)
		}},
	}
	rows := bytes.Split(bytes.TrimSuffix(readCurrentFixture(t, "stop_compaction_failed.jsonl"), []byte{'\n'}), []byte{'\n'})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var content []byte
			for i, row := range rows {
				record := decodeFixtureRecord(t, row)
				if record.EventKind != test.kind {
					continue
				}
				test.edit(t, &record)
				content = append(bytes.Join(rows[:i], []byte{'\n'}), '\n')
				content = append(content, forgeCompactionRecord(t, record)...)
				break
			}
			if len(content) == 0 {
				t.Fatal("missing corruption fixture record")
			}
			content = append(content, []byte("\n{\"seq\":")...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err == nil {
				if test.kind != EventCompactionFailed {
					t.Fatal("statically invalid complete record reached tail repair planning")
				}
				_, err = NewReplayPlanner().Plan(loaded)
			}
			if err == nil {
				t.Fatal("accepted forged check evidence", test.name)
			}
			assertJournalBytes(t, repo, content)
		})
	}
}

func TestPreCompactReplayAccountsForFocusSeparators(t *testing.T) {
	ctx := context.Background()
	repo, lease := installCurrentFixture(t, readCurrentFixture(t, "stop_compaction_started.jsonl"))
	defer repo.Close()
	defer lease.Close()
	loaded := loadFixtureJournal(t, ctx, lease)
	start, err := DecodeCompactionStartedPayload(loaded.Records[11])
	requireNoError(t, err)
	first := start.PreCompact.checks[0]
	start.PreCompact.checks = make([]compactionHookReference, 4)
	for i, id := range []string{"one", "two", "three", "four"} {
		reference := first
		reference.Reference.ID = id
		start.PreCompact.checks[i] = reference
	}
	start.PreCompact.digest, err = start.PreCompact.computeDigest(start.ID, start.Scope, start.Source)
	requireNoError(t, err)
	startDraft, err := NewCompactionStartedDraft(start.Scope, start.ID, start.Source, start.PreCompact)
	requireNoError(t, err)
	for _, overLimit := range []bool{false, true} {
		prefix := append([]Record(nil), loaded.Records[:11]...)
		appendDraft := func(draft RecordDraft, err error) {
			t.Helper()
			requireNoError(t, err)
			seq := uint64(len(prefix) + 1)
			record, err := BuildRecord(loaded.Identity, draft, seq, loaded.Records[0].Timestamp, seq, 0, 1)
			requireNoError(t, err)
			record, _, err = EncodeRecord(record)
			requireNoError(t, err)
			prefix = append(prefix, record)
		}
		appendDraft(startDraft, nil)
		target, err := domain.NewPreCompactHookTarget(start.ID, start.Scope)
		requireNoError(t, err)
		for i := range start.PreCompact.checks {
			id, err := domain.NewPreCompactHookRunIdentity(target, start.PreCompact.Digest(), i)
			requireNoError(t, err)
			appendDraft(NewPreCompactHookStartedDraft(id))
			count := domain.MaxHookFeedbackBytes
			if i == len(start.PreCompact.checks)-1 && !overLimit {
				count -= 6
			}
			result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, StdoutBytes: int64(count + 128), CleanupConfirmed: true, Origin: "execution"}
			appendDraft(NewPreCompactHookFinishedDraft(result, strings.Repeat("x", count)))
		}
		appendDraft(NewPreCompactHookDecidedDraft(start.ID, start.Scope, start.PreCompact, len(start.PreCompact.checks), "", domain.HookOK))
		_, err = NewReplayPlanner().Plan(finalLoadResult(loaded.Identity, prefix, testThreadID, RepairReport{}))
		if overLimit != (err != nil) {
			t.Fatalf("focus separator budget rejected=%v, over limit=%v", err, overLimit)
		}
	}
}

func forgeCompactionRecord(t *testing.T, record Record) []byte {
	t.Helper()
	digest, err := recordChecksum(record)
	requireNoError(t, err)
	record.Checksum = digest
	encoded, err := codec.MarshalStable(record)
	requireNoError(t, err)
	return encoded
}
