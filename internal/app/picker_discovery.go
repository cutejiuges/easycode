package app

import (
	"context"
	"easycode/internal/session/catalog"
	"easycode/internal/tui"
)

// pickerDiscovery 由串行启动 owner 独占，只有成功协调后才复用索引。
type pickerDiscovery struct {
	paths        dataPaths
	cwd          string
	initialized  bool
	report       catalog.ReconciliationReport
	dependencies *sessionListDependencies
}

func (d *pickerDiscovery) list(ctx context.Context, request tui.PickerRequest) (result tui.PickerResult, err error) {
	result.Request = request
	query := catalog.ListQuery{Scope: catalog.ListAllDirectories, Limit: 200}
	if !request.All {
		query.Scope = catalog.ListCurrentDirectory
		query.CreationCWD = d.cwd
	}
	deps := defaultSessionListDependencies()
	if d.dependencies != nil {
		deps = *d.dependencies
	}
	reconcile := !d.initialized || request.Operation == tui.PickerRefresh
	snapshot, err := loadSessionCatalog(ctx, d.paths, query, deps, reconcile)
	result.Fatal = snapshot.cleanupFailed
	if err != nil {
		return result, err
	}
	if reconcile {
		d.initialized = true
		d.report = snapshot.report
	}
	for _, e := range snapshot.rows.Entries {
		result.Entries = append(result.Entries, tui.SessionEntry{ThreadID: e.ThreadID, UpdatedAt: e.UpdatedAt, Family: e.ProviderFamily, Wire: e.ProviderWire, Model: e.Model, CWD: e.CreationCWD})
	}
	result.HasMore = snapshot.rows.HasMore
	result.Warning = formatSessionListWarning(d.report)
	return result, nil
}
