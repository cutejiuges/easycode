package session

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

func compactionReplayFixture(t *testing.T) (*replayFixture, RecordDraft, RecordDraft, RecordDraft) {
	t.Helper()
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	fixture.appendInitialInputs(testTurnID)
	fixture.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "cancelled"}))
	source, err := provider.NewCompactionSource(domain.ProviderOpenAI, "responses", "fixture-model", 1, 1, provider.RootCompactionPredecessor(), strings.Repeat("a", 64), provider.CompactionPromptRevision)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.CompactionID("01900000-0000-7000-8000-000000000009")
	start, err := NewCompactionStartedDraft(id, source)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := provider.NewCheckpointEnvelope(id, source, 3, []byte(`{"fixture":"opaque"}`))
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(100), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(10), domain.UnknownUsageMetric())
	before, _ := domain.NewEstimatedTokenEstimate("byte_heuristic_v1", 100)
	after, _ := domain.NewEstimatedTokenEstimate("byte_heuristic_v1", 20)
	complete, err := NewCompactionCompletedDraft(checkpoint, usage, before, after)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := NewCompactionFailedDraft(CompactionFailedPayload{ID: id, Code: protocol.CompactionFailureCancelled, UsageState: "unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, start, complete, failed
}

func TestCompactionReplayGrammar(t *testing.T) {
	for _, test := range []struct {
		name        string
		order       []int
		valid       bool
		interrupted bool
	}{
		{"completed", []int{0, 1}, true, false}, {"failed", []int{0, 2}, true, false}, {"started only", []int{0}, true, true},
		{"orphan completion", []int{1}, false, false}, {"orphan failure", []int{2}, false, false}, {"overlap", []int{0, 0}, false, false}, {"duplicate terminal", []int{0, 1, 2}, false, false}, {"reused identity", []int{0, 2, 0}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, start, complete, failed := compactionReplayFixture(t)
			choices := []RecordDraft{start, complete, failed}
			for _, index := range test.order {
				fixture.appendBatch(choices[index])
			}
			plan, err := NewReplayPlanner().Plan(fixture.loaded())
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t err=%v", test.valid, err)
			}
			if test.valid {
				if (plan.InterruptedCompaction != nil) != test.interrupted || len(plan.NativeCommits) != 1 || len(plan.SampleUsages) != 0 {
					t.Fatal("compaction polluted native commits or ordinary usage")
				}
				if !test.interrupted && len(plan.Compactions) != 1 {
					t.Fatal("compaction boundary missing")
				}
			}
		})
	}
	fixture, start, _, _ := compactionReplayFixture(t)
	fixture.appendBatch(start, start)
	if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil {
		t.Fatal("multi-record start batch accepted")
	}
	fixture, start, _, _ = compactionReplayFixture(t)
	fixture.appendBatch(start)
	fixture.appendBatch(mustDraft(t, EventTurnStarted, "01900000-0000-7000-8000-000000000099", TurnStartedPayload{}))
	if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil {
		t.Fatal("turn overlapped compaction")
	}
}

func TestCompactionPayloadStrictShape(t *testing.T) {
	fixture, start, complete, failed := compactionReplayFixture(t)
	for _, draft := range []RecordDraft{start, complete, failed} {
		fixture.appendBatch(draft)
		record := fixture.records[len(fixture.records)-1]
		_, encoded, err := EncodeRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeRecord(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.TurnID != "" {
			t.Fatal("compaction acquired a turn")
		}
		record.TurnID = testTurnID
		if err := decodeKnownRecord(record); err == nil {
			t.Fatal("compaction with turn identity accepted")
		}
	}
	record := Record{PayloadVersion: EnvelopeVersion, EventKind: EventCompactionFailed}
	for _, body := range []string{
		`{"compaction_id":"01900000-0000-7000-8000-000000000009","code":"compaction_cancelled","usage_state":"unavailable","usage":{}}`,
		`{"compaction_id":"01900000-0000-7000-8000-000000000009","code":"compaction_cancelled","usage_state":"observed"}`,
		`{"compaction_id":"01900000-0000-7000-8000-000000000009","code":"unknown","usage_state":"unavailable"}`,
		`{"compaction_id":null,"code":"compaction_cancelled","usage_state":"unavailable"}`,
	} {
		record.Payload = []byte(body)
		if _, err := DecodeCompactionFailedPayload(record); err == nil {
			t.Fatal("invalid failure shape accepted")
		}
	}
}
