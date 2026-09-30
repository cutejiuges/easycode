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
	report := ReconciliationReport{Present: len(locations)}
	present := make(map[domain.ThreadID]struct{}, len(locations))
	valid := make([]Entry, 0, len(locations))
	invalid := make(map[domain.ThreadID]struct{})
	loader := session.NewLoader()
	planner := session.NewReplayPlanner()
	projector := NewProjector()
	for _, location := range locations {
		present[location.ThreadID] = struct{}{}
		lease, openErr := repository.Open(ctx, location.ThreadID)
		if openErr != nil {
			if session.IsJournalBusy(openErr) {
				report.Busy++
				indexed, indexedErr := catalog.hasThread(location.ThreadID)
				if indexedErr != nil {
					return ReconciliationReport{}, indexedErr
				}
				if !indexed {
					report.BusyUnindexed = true
				}
				continue
			}
			if errors.Is(openErr, fs.ErrNotExist) {
				invalid[location.ThreadID] = struct{}{}
				report.Invalid++
				continue
			}
			return ReconciliationReport{}, fmt.Errorf("open session journal for catalog: %w", openErr)
		}
		loaded, loadErr := loader.Load(ctx, lease)
		if loadErr != nil {
			if closeErr := lease.Close(); closeErr != nil {
				return ReconciliationReport{}, fmt.Errorf("close invalid session journal: %w", closeErr)
			}
			invalid[location.ThreadID] = struct{}{}
			report.Invalid++
			continue
		}
		plan, planErr := planner.Plan(loaded)
		if planErr != nil {
			if closeErr := lease.Close(); closeErr != nil {
				return ReconciliationReport{}, fmt.Errorf("close invalid session journal: %w", closeErr)
			}
			invalid[location.ThreadID] = struct{}{}
			report.Invalid++
			continue
		}
		entry, projectErr := projector.Project(location.RelativePath, loaded, plan)
		if closeErr := lease.Close(); closeErr != nil {
			return ReconciliationReport{}, fmt.Errorf("close projected session journal: %w", closeErr)
		}
		if projectErr != nil {
			invalid[location.ThreadID] = struct{}{}
			report.Invalid++
			continue
		}
		valid = append(valid, entry)
		report.Valid++
	}
	if err := catalog.applyReconciliation(ctx, valid, present, invalid); err != nil {
		return ReconciliationReport{}, err
	}
	return report, nil
}

func (catalog *Catalog) applyReconciliation(ctx context.Context, valid []Entry, present map[domain.ThreadID]struct{}, invalid map[domain.ThreadID]struct{}) (resultErr error) {
	indexed, err := catalog.indexedThreadIDs()
	if err != nil {
		return err
	}
	if err := catalog.beginWrite(ctx); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, catalog.db.Exec("ROLLBACK"))
		}
	}()
	for _, entry := range valid {
		if err := catalog.upsert(entry); err != nil {
			return err
		}
	}
	for threadID := range invalid {
		if err := catalog.deleteThread(threadID); err != nil {
			return err
		}
	}
	for _, threadID := range indexed {
		if _, exists := present[threadID]; exists {
			continue
		}
		if err := catalog.deleteThread(threadID); err != nil {
			return err
		}
	}
	if err := catalog.db.Exec("COMMIT"); err != nil {
		return fmt.Errorf("commit session catalog reconciliation: %w", err)
	}
	committed = true
	return nil
}

func (catalog *Catalog) hasThread(threadID domain.ThreadID) (bool, error) {
	statement, _, err := catalog.db.Prepare("SELECT 1 FROM threads WHERE thread_id=?1 LIMIT 1")
	if err != nil {
		return false, fmt.Errorf("prepare session catalog identity query: %w", err)
	}
	defer func() { _ = statement.Close() }()
	if err := statement.BindText(1, string(threadID)); err != nil {
		return false, fmt.Errorf("bind session catalog identity: %w", err)
	}
	found := statement.Step()
	if err := statement.Err(); err != nil {
		return false, fmt.Errorf("query session catalog identity: %w", err)
	}
	return found, nil
}

func (catalog *Catalog) indexedThreadIDs() ([]domain.ThreadID, error) {
	statement, _, err := catalog.db.Prepare("SELECT thread_id FROM threads ORDER BY thread_id")
	if err != nil {
		return nil, fmt.Errorf("prepare session catalog identities: %w", err)
	}
	defer func() { _ = statement.Close() }()
	identities := make([]domain.ThreadID, 0)
	for statement.Step() {
		threadID, parseErr := domain.ParseThreadID(statement.ColumnText(0))
		if parseErr != nil {
			return nil, fmt.Errorf("session catalog row is invalid")
		}
		identities = append(identities, threadID)
	}
	if err := statement.Err(); err != nil {
		return nil, fmt.Errorf("query session catalog identities: %w", err)
	}
	return identities, nil
}

func (catalog *Catalog) deleteThread(threadID domain.ThreadID) error {
	statement, _, err := catalog.db.Prepare("DELETE FROM threads WHERE thread_id=?1")
	if err != nil {
		return fmt.Errorf("prepare session catalog deletion: %w", err)
	}
	defer func() { _ = statement.Close() }()
	if err := statement.BindText(1, string(threadID)); err != nil {
		return fmt.Errorf("bind session catalog deletion: %w", err)
	}
	if err := statement.Exec(); err != nil {
		return fmt.Errorf("delete session catalog entry: %w", err)
	}
	return nil
}
