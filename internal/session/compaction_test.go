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
	source, err := provider.NewCompactionSource(domain.ProviderOpenAI, "responses", "fixture-model", 1, 1, provider.RootCompactionPredecessor(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	id := domain.CompactionID("01900000-0000-7000-8000-000000000009")
	start, err := NewCompactionStartedDraft(domain.ManualCompactionScope(), id, source)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := provider.NewCheckpointEnvelope(domain.ManualCompactionScope(), id, source, []byte(`{"fixture":"opaque"}`))
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(100), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(10), domain.UnknownUsageMetric())
	before, _ := domain.NewEstimatedTokenEstimate("byte_heuristic_v1", 100)
	after, _ := domain.NewEstimatedTokenEstimate("byte_heuristic_v1", 20)
	complete, err := NewCompactionCompletedDraft(domain.ManualCompactionScope(), checkpoint, usage, before, after)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := NewCompactionFailedDraft(CompactionFailedPayload{Scope: domain.ManualCompactionScope(), ID: id, Code: protocol.CompactionFailureCancelled, UsageState: "unavailable"})
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
	record := Record{EventKind: EventCompactionFailed}
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

func TestAutomaticCompactionRequiresExactSamplingBoundary(t *testing.T) {
	for _, mode := range []string{"initial", "outputs", "wrong_sample", "wrong_turn", "pending_calls", "input_saved", "outputs_missing", "repeated"} {
		t.Run(mode, func(t *testing.T) {
			fixture, _, _, _ := compactionReplayFixture(t)
			plan, err := NewReplayPlanner().Plan(fixture.loaded())
			if err != nil {
				t.Fatal(err)
			}
			source, _ := provider.NewCompactionSource(domain.ProviderOpenAI, "responses", "fixture-model", 1, 1, provider.RootCompactionPredecessor(), strings.Repeat("a", 64))
			turn := &activeReplayTurn{turnID: testTurnID}
			scope, _ := domain.NewAutoCompactionScope(testTurnID, 0)
			switch mode {
			case "outputs":
				turn.initialInputs = true
				turn.hadToolSample = true
				turn.toolOutputsCommitted = true
				turn.nextSampleIndex = 1
				scope.SampleIndex = 1
			case "wrong_sample":
				scope.SampleIndex = 1
			case "wrong_turn":
				scope.TurnID = secondTurnID
			case "pending_calls":
				turn.pendingCalls = []ReplayedToolCall{{}}
			case "input_saved":
				turn.inputAtSample = true
			case "outputs_missing":
				turn.initialInputs = true
				turn.hadToolSample = true
			case "repeated":
				turn.autoCompactedSample = true
			}
			draft, err := NewCompactionStartedDraft(scope, "01900000-0000-7000-8000-000000000009", source)
			if err != nil {
				t.Fatal(err)
			}
			record := Record{EventKind: EventCompactionStarted, Payload: draft.PayloadBytes()}
			state := compactionReplay{seen: make(map[domain.CompactionID]struct{})}
			err = state.accept(&plan, []Record{record}, turn)
			if (err == nil) != (mode == "initial" || mode == "outputs") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}
