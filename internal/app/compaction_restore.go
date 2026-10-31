package app

import (
	"context"
	"fmt"
	"sort"

	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// orderedRestoreRecords 保留原生 ordinal，并将来源校验与 checkpoint 按 journal seq 插入。
func orderedRestoreRecords(plan session.ReplayPlan) ([]provider.RestoreRecord, error) {
	ordinary, err := nativeRestoreRecords(plan.NativeCommits)
	if err != nil {
		return nil, err
	}
	type entry struct {
		sequence uint64
		record   provider.RestoreRecord
	}
	entries := make([]entry, 0, len(ordinary)+len(plan.Compactions)*2+1)
	for i, record := range ordinary {
		entries = append(entries, entry{plan.NativeCommits[i].Sequence, record})
	}
	compactions := append([]session.ReplayedCompaction{}, plan.Compactions...)
	if plan.InterruptedCompaction != nil {
		compactions = append(compactions, *plan.InterruptedCompaction)
	}
	for _, operation := range compactions {
		source, err := provider.NewCompactionSourceRestoreRecord(operation.Started.Source)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{operation.StartedSequence, source})
		if operation.Completed != nil {
			checkpoint, err := provider.NewCheckpointRestoreRecord(operation.Completed.Checkpoint)
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
	return result, nil
}

func validateCompactionUsage(factory provider.Factory, plan session.ReplayPlan) error {
	for _, operation := range plan.Compactions {
		if operation.Completed != nil {
			validator, ok := factory.(provider.CompactionUsageValidator)
			if !ok {
				return fmt.Errorf("provider cannot validate checkpoint usage")
			}
			usage, err := operation.Completed.Usage.Domain()
			if err != nil {
				return err
			}
			if err := validator.ValidateCheckpointUsage(operation.Completed.Checkpoint, usage); err != nil {
				return err
			}
		}
		if operation.Failed == nil || operation.Failed.UsageState != "observed" {
			continue
		}
		validator, ok := factory.(provider.CompactionUsageValidator)
		if !ok {
			return fmt.Errorf("provider cannot validate compaction usage")
		}
		raw := operation.Failed.Provenance
		envelope, err := provider.NewNativeCommitEnvelope(raw.Provider, raw.Wire, raw.PayloadVersion, raw.Payload)
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
	return nil
}

func reconcileInterruptedCompaction(ctx context.Context, writer managedJournal, operation *session.ReplayedCompaction) error {
	if operation == nil {
		return nil
	}
	draft, err := session.NewCompactionFailedDraft(session.CompactionFailedPayload{ID: operation.Started.ID, Code: protocol.CompactionFailureInterrupted, UsageState: "unavailable"})
	if err != nil {
		return err
	}
	_, err = writer.AppendBatch(ctx, []session.RecordDraft{draft})
	return err
}
