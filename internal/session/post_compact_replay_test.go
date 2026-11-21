package session

import (
	"bytes"
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestPostCompactReplayPreservesCheckpointAndStopAcrossRecovery(t *testing.T) {
	content := readCurrentFixture(t, "stop_post_compact_completed.jsonl")
	repo, lease := installCurrentFixture(t, content)
	defer repo.Close()
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil || loaded.NeedsRepair() {
		t.Fatal("invalid current fixture", err)
	}
	planner := NewReplayPlanner()
	tests := []struct {
		sequence uint64
		state    protocol.PostCompactStatus
		code     domain.HookCode
		finished int
	}{
		{13, protocol.PostCompactInterrupted, domain.HookInterrupted, 0},
		{14, protocol.PostCompactUncertain, domain.HookOutcomeUncertain, 0},
		{15, protocol.PostCompactInterrupted, domain.HookInterrupted, 1},
		{16, protocol.PostCompactUncertain, domain.HookOutcomeUncertain, 1},
		{17, protocol.PostCompactFailed, domain.HookExitFailed, 2},
		{18, "", "", 2},
	}
	for _, test := range tests {
		prefix := finalLoadResult(loaded.Identity, loaded.Records[:test.sequence], testThreadID, RepairReport{})
		plan, err := planner.Plan(prefix)
		if err != nil || plan.InterruptedCompaction != nil || len(plan.Compactions) != 1 ||
			plan.Compactions[0].TerminalSequence != 13 || plan.Compactions[0].Completed == nil ||
			plan.StopRecovery == nil || !plan.StopRecovery.State().PendingFeedback() || len(plan.NativeCommits) != 2 {
			t.Fatalf("sequence %d did not preserve checkpoint and pending Stop feedback: %v", test.sequence, err)
		}
		if test.state == "" {
			if plan.InterruptedPostCompact != nil {
				t.Fatal("closed observer was scheduled for recovery")
			}
			continue
		}
		if plan.InterruptedPostCompact == nil {
			t.Fatalf("sequence %d lost its observation prefix", test.sequence)
		}
		draft, err := plan.InterruptedPostCompact.CloseDraft()
		if err != nil {
			t.Fatal(err)
		}
		sequence := test.sequence + 1
		record, err := BuildRecord(loaded.Identity, draft, sequence, loaded.Records[0].Timestamp, sequence, 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		closed, err := DecodePostCompactHookClosedPayload(record)
		if err != nil || closed.State != test.state || closed.Code != test.code || closed.Finished != test.finished || closed.Origin != postCompactRecovery {
			t.Fatalf("sequence %d closed as %+v: %v", test.sequence, closed, err)
		}
		recovered := append(append([]Record(nil), prefix.Records...), record)
		after, err := planner.Plan(finalLoadResult(loaded.Identity, recovered, testThreadID, RepairReport{}))
		if err != nil || after.InterruptedPostCompact != nil || after.StopRecovery == nil || !after.StopRecovery.State().PendingFeedback() {
			t.Fatalf("sequence %d did not consume exactly the observer tail: %v", test.sequence, err)
		}
	}
	for _, record := range loaded.Records[12:] {
		if _, encoded, err := EncodeRecord(record); err != nil || !bytes.Contains(content, append(encoded, '\n')) {
			t.Fatalf("current fixture record is not canonical: %v", err)
		}
	}
}

func TestPostCompactReplayRejectsInvalidCompleteFactsBeforeRepair(t *testing.T) {
	tests := []struct {
		name      string
		index     int
		inDecoder bool
		mutate    func(*Record)
	}{
		{"completed batch", 12, true, func(record *Record) { record.BatchSize = 2 }},
		{"started batch", 13, true, func(record *Record) { record.BatchSize = 2 }},
		{"stale checkpoint digest", 12, true, func(record *Record) {
			record.Payload = bytes.Replace(record.Payload, []byte(`"fixture":"opaque"`), []byte(`"fixture":"forged"`), 1)
		}},
		{"impossible success", 16, true, func(record *Record) {
			record.Payload = bytes.Replace(record.Payload, []byte(`"stdout_bytes":0`), []byte(`"stdout_bytes":16385`), 1)
		}},
		{"wrong finished count", 17, false, func(record *Record) {
			record.Payload = bytes.Replace(record.Payload, []byte(`"finished":2`), []byte(`"finished":1`), 1)
		}},
	}
	rows := bytes.Split(bytes.TrimSuffix(readCurrentFixture(t, "stop_post_compact_completed.jsonl"), []byte{'\n'}), []byte{'\n'})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, err := DecodeRecord(rows[test.index])
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&record)
			if record.BatchSize == 1 && bytes.Contains(rows[test.index], record.Payload) {
				t.Fatal("invalid test did not change the record")
			}
			content := append(bytes.Join(rows[:test.index], []byte{'\n'}), '\n')
			content = append(content, forgeCompactionRecord(t, record)...)
			content = append(content, "\n{\"seq\":"...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if test.inDecoder {
				if err == nil {
					t.Fatal("invalid complete line reached tail repair")
				}
				return
			}
			if err != nil || !loaded.NeedsRepair() {
				t.Fatal("expected valid envelope and a repair ticket", err)
			}
			planner := NewReplayPlanner()
			if _, err := planner.Plan(loaded); err == nil {
				t.Fatal("replay accepted an observer closure inconsistent with its prefix")
			}
			if _, err := planner.PlanMetadata(loaded); err == nil {
				t.Fatal("metadata replay accepted an observer closure inconsistent with its prefix")
			}
		})
	}
}
