package app

import (
	"context"
	"fmt"
	"sort"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// orderedRestoreRecords 保留原生 ordinal，并将来源校验与 checkpoint 按 journal seq 插入。
func orderedRestoreRecords(ctx context.Context, plan session.ReplayPlan) ([]provider.RestoreRecord, error) {
	ordinary, err := nativeRestoreRecords(ctx, plan.NativeCommits)
	if err != nil {
		return nil, err
	}
	type entry struct {
		sequence uint64
		record   provider.RestoreRecord
	}
	entries := make([]entry, 0, len(ordinary)+len(plan.Compactions)*2+1)
	type readyFacts struct {
		calls   []tool.Invocation
		results []tool.InvocationResult
	}
	callsBySequence := make(map[uint64]readyFacts)
	for _, entry := range plan.Tools {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, isQuestion := entry.Invocation.Question()
		facts := callsBySequence[entry.NativeSequence]
		facts.calls = append(facts.calls, entry.Invocation.Clone())
		if (isQuestion || entry.Invocation.Capability() == tool.CapabilityTaskProgress) && entry.HasResult {
			facts.results = append(facts.results, entry.Result)
		}
		callsBySequence[entry.NativeSequence] = facts
	}
	for i, record := range ordinary {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if record.Role() == domain.NativeCommitSample {
			facts := callsBySequence[plan.NativeCommits[i].Sequence]
			record, err = record.WithReadyFacts(facts.calls, facts.results)
			if err != nil {
				return nil, err
			}
		}
		entries = append(entries, entry{plan.NativeCommits[i].Sequence, record})
	}
	compactions := append([]session.ReplayedCompaction{}, plan.Compactions...)
	if plan.InterruptedCompaction != nil {
		compactions = append(compactions, *plan.InterruptedCompaction)
	}
	for _, operation := range compactions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, err := provider.NewCompactionSourceRestoreRecord(operation.Started.Source)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{operation.StartedSequence, source})
		if operation.Completed != nil {
			expected, err := operation.TaskProgressAtStart()
			if err != nil {
				return nil, err
			}
			checkpoint, err := provider.NewCheckpointRestoreRecord(operation.Completed.Checkpoint, expected)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry{operation.TerminalSequence, checkpoint})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].sequence < entries[j].sequence })
	result := make([]provider.RestoreRecord, len(entries))
	for i, entry := range entries {
		result[i] = entry.record
	}
	return result, ctx.Err()
}

func validateCompactionUsageEvidence(ctx context.Context, validator provider.CompactionUsageValidator, checkpoints provider.CheckpointReader, plan session.ReplayPlan) error {
	for _, operation := range plan.Compactions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if operation.Completed != nil {
			if checkpoints == nil {
				return fmt.Errorf("provider cannot validate checkpoint usage")
			}
			usage, err := operation.Completed.Usage.Domain()
			if err != nil {
				return err
			}
			if err := checkpoints.ValidateCheckpointUsage(operation.Completed.Checkpoint, usage); err != nil {
				return err
			}
		}
		if operation.Failed == nil || operation.Failed.UsageState != "observed" {
			continue
		}
		if validator == nil {
			return fmt.Errorf("provider cannot validate compaction usage")
		}
		raw := operation.Failed.Provenance
		envelope, err := provider.NewNativeCommitEnvelope(raw.Provider, raw.Wire, raw.Payload)
		if err != nil {
			return err
		}
		usage, err := operation.Failed.Usage.Domain()
		if err != nil {
			return err
		}
		if err := validator.ValidateCompactionUsage(envelope, usage); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func reconcileInterruptedCompaction(ctx context.Context, writer managedJournal, operation *session.ReplayedCompaction) error {
	if operation == nil {
		return nil
	}
	draft, err := session.NewCompactionFailedDraft(session.CompactionFailedPayload{Scope: operation.Started.Scope, ID: operation.Started.ID, Code: protocol.CompactionFailureInterrupted, UsageState: "unavailable"})
	if err != nil {
		return err
	}
	_, err = writer.AppendBatch(ctx, []session.RecordDraft{draft})
	return err
}
