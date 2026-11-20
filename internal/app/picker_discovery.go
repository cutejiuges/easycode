package app

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/session/catalog"
	"easycode/internal/tui"
)

// pickerDiscovery 由串行启动 owner 独占，两个范围共用最近一次成功协调的快照。
type pickerDiscovery struct {
	paths        dataPaths
	cwd          string
	source       domain.ThreadID
	snapshot     *catalogSnapshot
	dependencies *sessionListDependencies
}

func (d *pickerDiscovery) list(ctx context.Context, request tui.PickerRequest) (result tui.PickerResult, err error) {
	result.Request = request
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if d.snapshot == nil || d.source != request.Source || request.Operation == tui.PickerRefresh {
		deps := defaultSessionListDependencies()
		if d.dependencies != nil {
			deps = *d.dependencies
		}
		current := catalog.ListQuery{Scope: catalog.ListCurrentDirectory, CreationCWD: d.cwd, ExcludeThreadID: request.Source, Limit: catalog.MaximumListLimit}
		all := catalog.ListQuery{Scope: catalog.ListAllDirectories, ExcludeThreadID: request.Source, Limit: catalog.MaximumListLimit}
		snapshot, err := loadSessionCatalog(ctx, d.paths, deps, current, all)
		result.Fatal = snapshot.cleanupFailed
		if err != nil {
			return result, err
		}
		d.source, d.snapshot = request.Source, &snapshot
	}
	rows := d.snapshot.rows[0]
	if request.All {
		rows = d.snapshot.rows[1]
	}
	for _, e := range rows.Entries {
		result.Entries = append(result.Entries, tui.SessionEntry{
			ThreadID: e.ThreadID, UpdatedAt: e.UpdatedAt, Title: e.Title,
			Family: e.ProviderFamily, Wire: e.ProviderWire, Model: e.Model, CWD: e.CreationCWD,
		})
	}
	result.HasMore = rows.HasMore
	result.Warning = formatSessionListWarning(d.snapshot.report)
	return result, nil
}
