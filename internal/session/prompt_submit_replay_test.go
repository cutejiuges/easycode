package session

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestPromptSubmitReplayBindsSessionStartAndStopInput(t *testing.T) {
	stop := append(readCurrentFixture(t, "stop_continued.jsonl"), readCurrentFixture(t, "prompt_submit_stop_delivered_suffix.jsonl")...)
	for _, test := range []struct {
		name   string
		data   []byte
		digest int
		role   domain.NativeCommitRole
		seq    uint64
	}{
		{"session start", readCurrentFixture(t, "prompt_submit_initial_session_start.jsonl"), 3, domain.NativeCommitSessionStartContext, 15},
		{"stop correction", stop, 11, domain.NativeCommitHookFeedback, 17},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, lease := installCurrentFixture(t, test.data)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil || loaded.NeedsRepair() {
				t.Fatalf("load complete prompt fixture: %v", err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil || plan.InterruptedPromptSubmit != nil || len(plan.NativeCommits) < 2 {
				t.Fatalf("verify prompt delivery: %v", err)
			}
			commits := plan.NativeCommits[len(plan.NativeCommits)-2:]
			delivery := commits[1]
			check, err := DecodePromptSubmitPlanned(loaded.Records[test.digest])
			requireNoError(t, err)
			want := check.fields.InputIDs[len(check.fields.InputIDs)-1:]
			if commits[0].Role != test.role || commits[0].Sequence != test.seq ||
				delivery.Role != domain.NativeCommitUserInputs || delivery.Sequence != test.seq+2 ||
				!slices.Equal(delivery.Inputs.IDs(), want) {
				t.Fatal("prompt delivery changed native association, cursor or order")
			}
			if _, err := NewReplayPlanner().PlanMetadata(loaded); err != nil {
				t.Fatalf("metadata path disagreed with full replay: %v", err)
			}

			// 用错误的输入身份替换现有复合批次中的关联；原生正文保持原值。
			association := &loaded.Records[len(loaded.Records)-2]
			wrong := TurnInputsPayload{
				Placement: check.fields.Placement, TargetSampleIndex: check.fields.TargetSampleIndex,
				InputIDs: []domain.InputID{"00000000-0010-7000-8000-000000000010"},
			}
			if len(check.fields.InputIDs) > 1 {
				wrong.InputIDs[0] = check.fields.InputIDs[0]
			}
			association.Payload, err = codec.MarshalStable(wrong)
			requireNoError(t, err)
			if _, err := NewReplayPlanner().PlanMetadata(loaded); err == nil {
				t.Fatal("metadata replay accepted a nonapproved input association")
			}
		})
	}
}

func TestPromptSubmitRecoveryClosesStartedRunOnce(t *testing.T) {
	ctx := context.Background()
	baseline := readCurrentFixture(t, "stop_continued.jsonl")
	rows := bytes.SplitN(readCurrentFixture(t, "prompt_submit_stop_delivered_suffix.jsonl"), []byte{'\n'}, 3)
	started := append(slices.Clone(baseline), bytes.Join(rows[:2], []byte{'\n'})...)
	started = append(started, '\n')
	t.Run("recover only the missing prefix", func(t *testing.T) {
		repo, lease := installCurrentFixture(t, append(slices.Clone(started), []byte(`{"seq":`)...))
		defer repo.Close()
		defer func() { _ = lease.Close() }()
		loader, planner := NewLoader(), NewReplayPlanner()
		loaded, err := loader.Load(ctx, lease)
		if err != nil || !loaded.NeedsRepair() {
			t.Fatalf("load torn recovery input: %v", err)
		}
		plan, err := planner.Plan(loaded)
		if err != nil || plan.InterruptedPromptSubmit == nil || plan.StopRecovery == nil ||
			!plan.StopRecovery.State().PendingFeedback() || len(plan.NativeCommits) != 2 {
			t.Fatalf("recover prompt and preserve saved Stop obligation: %v", err)
		}
		drafts, err := plan.InterruptedPromptSubmit.CloseDrafts()
		if err != nil || len(drafts) != 2 {
			t.Fatalf("expected first recovery to close execution and plan: %v", err)
		}
		if report, err := loader.Repair(ctx, lease, loaded); err != nil || !report.Repaired {
			t.Fatalf("remove only the torn suffix: %v", err)
		}
		appendRecovery := func(loaded LoadResult, draft RecordDraft) LoadResult {
			writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
			requireNoError(t, err)
			_, appendErr := writer.AppendBatch(ctx, []RecordDraft{draft})
			if err := writer.Close(ctx); err != nil || appendErr != nil {
				t.Fatalf("persist recovery fact: %v, %v", appendErr, err)
			}
			lease, err = repo.Open(ctx, testThreadID)
			requireNoError(t, err)
			partial, err := loader.Load(ctx, lease)
			requireNoError(t, err)
			return partial
		}
		partial := appendRecovery(loaded, drafts[0])
		plan, err = planner.Plan(partial)
		if err != nil || plan.InterruptedPromptSubmit == nil || plan.StopRecovery == nil {
			t.Fatalf("second crash changed pending ownership: %v", err)
		}
		drafts, err = plan.InterruptedPromptSubmit.CloseDrafts()
		if err != nil || len(drafts) != 1 {
			t.Fatalf("second recovery attempted to repeat a finished run: %v", err)
		}
		partial = appendRecovery(partial, drafts[0])
		plan, err = planner.Plan(partial)
		if err != nil || plan.InterruptedPromptSubmit != nil || plan.StopRecovery == nil ||
			!plan.StopRecovery.State().PendingFeedback() || len(plan.NativeCommits) != 2 {
			t.Fatalf("recovery altered the Stop obligation or synthesized input: %v", err)
		}
	})
	t.Run("complete illegal arity is corruption", func(t *testing.T) {
		content := append(baseline, readCurrentFixture(t, "prompt_submit_forged_arity_suffix.jsonl")...)
		repo, lease := installCurrentFixture(t, content)
		defer repo.Close()
		defer lease.Close()
		if _, err := NewLoader().Load(ctx, lease); err == nil {
			t.Fatal("checksum-valid prompt start with forged arity was accepted as repairable")
		}
		assertJournalBytes(t, repo, content)
	})
}

func TestPromptSubmitValidatesVisibleInputAssociationBeforeTailRepair(t *testing.T) {
	ctx := context.Background()
	rows := bytes.Split(readCurrentFixture(t, "prompt_submit_initial_session_start.jsonl"), []byte{'\n'})
	association := decodeFixtureRecord(t, rows[15])
	payload, err := DecodeTurnInputsPayload(association)
	requireNoError(t, err)
	checkRecord := decodeFixtureRecord(t, rows[3])
	check, err := DecodePromptSubmitPlanned(checkRecord)
	requireNoError(t, err)

	for _, probe := range []struct {
		name     string
		compound bool
		terminal bool
		forged   bool
	}{
		{name: "forged ordinary", forged: true},
		{name: "forged activation compound", compound: true, forged: true},
		{name: "forged reuse after turn terminated", terminal: true, forged: true},
		{name: "legal torn pair"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			tail, inputs := association, payload
			prefixEnd := 15
			if !probe.compound {
				prefixEnd = 11
				tail.Sequence, tail.BatchID, tail.BatchSize, tail.BatchIndex = 12, 12, 2, 0
			}
			if probe.forged {
				inputs.InputIDs = check.fields.InputIDs[:1]
			}
			prefix := append(bytes.Join(rows[:prefixEnd], []byte{'\n'}), '\n')
			if probe.terminal {
				draft, err := NewTurnFailedDraft(tail.TurnID, TurnFailedPayload{Code: "user_cancelled", Cancelled: true})
				requireNoError(t, err)
				prefix = appendStartupTestRecord(t, prefix, tail, draft)
				tail.Sequence++
				tail.BatchID++
			}
			encoded, err := codec.MarshalStable(inputs)
			requireNoError(t, err)
			tail.Payload = encoded
			_, line, err := EncodeRecord(tail)
			requireNoError(t, err)
			content := append(slices.Clone(prefix), append(line, '\n')...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loader := NewLoader()
			loaded, err := loader.Load(ctx, lease)
			if err != nil || !loaded.NeedsRepair() {
				t.Fatalf("load visible tail association: %v", err)
			}
			planner := NewReplayPlanner()
			planned, err := planner.Plan(loaded)
			if probe.forged {
				if err == nil {
					t.Fatal("replay accepted a denied InputID in a checksum-valid torn batch")
				}
				if _, err = planner.PlanMetadata(loaded); err == nil {
					t.Fatal("metadata replay accepted a denied InputID in a torn batch")
				}
				assertJournalBytes(t, repo, content)
				return
			}
			if err != nil || len(planned.NativeCommits) != 0 || planned.InterruptedTail == nil {
				t.Fatalf("legal torn pair fabricated native input or lost its turn: %v", err)
			}
			if report, err := loader.Repair(ctx, lease, loaded); err != nil || !report.Repaired {
				t.Fatalf("repair did not remove the legal incomplete batch: %v", err)
			}
			assertJournalBytes(t, repo, prefix)
		})
	}
}
