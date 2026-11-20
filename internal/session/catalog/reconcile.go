package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"easycode/internal/domain"
	"easycode/internal/session"
)

// Reconcile 从当前 JSONL 事实源完整重建本次可查询投影。
func (catalog *Catalog) Reconcile(ctx context.Context, repository *session.Repository) (ReconciliationReport, error) {
	if err := catalog.ready(ctx); err != nil {
		return ReconciliationReport{}, err
	}
	if repository == nil {
		return ReconciliationReport{}, fmt.Errorf("session repository is required")
	}
	locations, err := repository.EnumerateJournals(ctx)
	if err != nil {
		return ReconciliationReport{}, fmt.Errorf("enumerate session journals: %w", err)
	}

	previous := make(map[domain.ThreadID]Entry, len(catalog.entries))
	for _, entry := range catalog.entries {
		previous[entry.ThreadID] = entry
	}
	next := make([]Entry, 0, len(locations))
	report := ReconciliationReport{Present: len(locations)}
	loader := session.NewLoader()
	planner := session.NewReplayPlanner()
	projector := NewProjector()
	for _, location := range locations {
		lease, err := repository.Open(ctx, location.ThreadID)
		if err != nil {
			switch {
			case session.IsJournalBusy(err):
				report.Busy++
				if entry, exists := previous[location.ThreadID]; exists {
					next = append(next, entry)
				} else {
					report.BusyUnindexed = true
				}
			case errors.Is(err, fs.ErrNotExist):
				report.Invalid++
			default:
				return ReconciliationReport{}, fmt.Errorf("open session journal for catalog: %w", err)
			}
			continue
		}

		loaded, loadErr := loader.Load(ctx, lease)
		entry, projectErr := Entry{}, loadErr
		if loadErr == nil {
			plan, planErr := planner.PlanMetadata(loaded)
			projectErr = planErr
			if planErr == nil {
				entry, projectErr = projector.Project(location.RelativePath, loaded, plan)
			}
		}
		if err := lease.Close(); err != nil {
			return ReconciliationReport{}, fmt.Errorf("close session journal for catalog: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return ReconciliationReport{}, err
		}
		if projectErr != nil {
			report.Invalid++
			continue
		}
		next = append(next, entry)
		report.Valid++
	}
	if err := catalog.publish(ctx, next); err != nil {
		return ReconciliationReport{}, err
	}
	return report, nil
}
