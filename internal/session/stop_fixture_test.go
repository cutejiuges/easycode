package session

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type stopFixtureExpected struct {
	Name        string                 `json:"name"`
	Stops       int                    `json:"stops"`
	Native      int                    `json:"native"`
	Samples     int                    `json:"samples"`
	Processed   int                    `json:"processed"`
	Started     bool                   `json:"started"`
	Pending     bool                   `json:"pending"`
	Open        bool                   `json:"open"`
	Compaction  bool                   `json:"compaction"`
	Disposition domain.StopDisposition `json:"disposition"`
}

func TestCurrentStopFixturesAreFixedAndStrict(t *testing.T) {
	expected, err := os.ReadFile("testdata/current/stop_replay.expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []stopFixtureExpected
	if err := codec.UnmarshalRecord(expected, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fixed := readCurrentFixture(t, "stop_"+tc.Name+".jsonl")
			for _, line := range bytes.Split(bytes.TrimSuffix(fixed, []byte("\n")), []byte("\n")) {
				record, err := DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				_, encoded, err := EncodeRecord(record)
				if err != nil || !bytes.Equal(line, encoded) {
					t.Fatal("current bytes changed", err)
				}
			}
			repo, lease := installCurrentFixture(t, fixed)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Stops) != tc.Stops || len(plan.NativeCommits) != tc.Native || len(plan.SampleUsages) != tc.Samples || (plan.StopRecovery != nil) != tc.Open || (plan.InterruptedCompaction != nil) != tc.Compaction || plan.ToolRecovery != nil || plan.InterruptedTail != nil || len(plan.Tools) != 0 {
				t.Fatal("incorrect replay shape", tc.Name)
			}
			state := plan.Stops[len(plan.Stops)-1]
			if len(state.Finished) != tc.Processed || (state.Started != nil) != tc.Started || state.PendingFeedback() != tc.Pending || state.Validate() != nil {
				t.Fatal("lost prefix", tc.Name)
			}
			if tc.Disposition != "" && (state.Decision == nil || state.Decision.Disposition != tc.Disposition) || tc.Disposition == "" && state.Decision != nil {
				t.Fatal("invented completion")
			}
			for _, commit := range plan.NativeCommits {
				if commit.Role == domain.NativeCommitHookFeedback {
					if commit.Feedback == nil || commit.Feedback.Validate() != nil || commit.Inputs.TurnID() != "" {
						t.Fatal("invented user input")
					}
				}
			}
		})
	}
}

func TestStopFixedSemanticCorruptionPrecedesTornTailRepair(t *testing.T) {
	for _, name := range []string{"invalid", "invalid_missing_delivery"} {
		t.Run(name, func(t *testing.T) {
			content := append(readCurrentFixture(t, "stop_"+name+".jsonl"), []byte(`{"seq":`)...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil || !loaded.NeedsRepair() {
				t.Fatal("missing torn tail", err)
			}
			if _, err := NewReplayPlanner().Plan(loaded); err == nil {
				t.Fatal("semantic corruption accepted")
			}
			path, _ := repo.JournalPath(testThreadID)
			after, _ := os.ReadFile(path)
			if !bytes.Equal(content, after) {
				t.Fatal("corrupt prefix was repaired")
			}
		})
	}
}

func TestStopLeaseAcrossProcessesRetainsRecoveryOwnership(t *testing.T) {
	for _, phase := range []string{"planned", "started", "prefix", "continued"} {
		t.Run(phase, func(t *testing.T) {
			fixed := readCurrentFixture(t, "stop_"+phase+".jsonl")
			content := append(append([]byte(nil), fixed...), []byte(`{"seq":`)...)
			root := createLeaseProcessJournal(t, content)
			repo, err := OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			helper := startJournalLeaseHelper(t, root)
			if _, err := repo.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
				t.Fatal("competing process acquired recovery lease", err)
			}
			path, _ := repo.JournalPath(testThreadID)
			during, _ := os.ReadFile(path)
			if !bytes.Equal(content, during) {
				t.Fatal("busy acquisition repaired bytes")
			}
			helper.kill(t)
			lease, err := repo.Open(context.Background(), testThreadID)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil || plan.StopRecovery == nil {
				t.Fatal(err)
			}
			if _, err := NewLoader().Repair(context.Background(), lease, loaded); err != nil {
				t.Fatal(err)
			}
			assertStopLeaseBusyChild(t, root)
			writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
			if err != nil {
				t.Fatal(err)
			}
			state := plan.StopRecovery.State()
			var draft RecordDraft
			if state.PendingFeedback() {
				draft = mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit())
			} else {
				id, _ := domain.NewStopHookRunIdentity(state.Plan, len(state.Finished))
				if state.Started == nil {
					draft, _ = NewHookExecutionStartedDraft(testTurnID, id)
				} else {
					result := testStopFinished(t, state.Plan, len(state.Finished), domain.HookUncertain, domain.HookOutcomeUncertain, "recovery")
					finished, _ := NewHookExecutionFinishedDraft(testTurnID, result)
					source, _ := domain.NewStopHookSource(len(state.Finished))
					facts := append(state.Finished, result)
					decision := testStopDecisionDraft(t, state.Plan, domain.StopUncertain, domain.HookOutcomeUncertain, domain.HookOriginRecovery, source, facts)
					records, err := writer.AppendBatch(context.Background(), []RecordDraft{finished, decision, mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: string(domain.HookOutcomeUncertain)})})
					if err != nil || records[0].Sequence != loaded.NextSequence {
						t.Fatal("nonmonotonic recovery", err)
					}
				}
			}
			if draft.EventKind() != "" {
				records, err := writer.AppendBatch(context.Background(), []RecordDraft{draft})
				if err != nil || records[0].Sequence != loaded.NextSequence {
					t.Fatal("nonmonotonic append", err)
				}
			}
			if err := writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.HasPrefix(after, fixed) {
				t.Fatal("append rewrote committed prefix")
			}
		})
	}
}

func TestStopLeaseBusyChild(t *testing.T) {
	root := os.Getenv("EASYCODE_TEST_STOP_LEASE_BUSY")
	if root == "" {
		return
	}
	repo, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if _, err := repo.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatal("child acquired owned journal", err)
	}
}
func assertStopLeaseBusyChild(t *testing.T, root string) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestStopLeaseBusyChild$")
	command.Env = append(os.Environ(), "EASYCODE_TEST_STOP_LEASE_BUSY="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal("child lease check failed", err, string(output))
	}
}
