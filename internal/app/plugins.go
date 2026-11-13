package app

import (
	"easycode/internal/domain"
	"easycode/internal/extension"
)

// pluginList 在 Skill 最终纳入后一次生成展示值，不重新发现来源。
func pluginList(sources []extension.PluginSource, summaries []extension.PluginSummary) ([]domain.PluginListEntry, error) {
	byID := make(map[string]extension.PluginSummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.ID] = summary
	}
	var out []domain.PluginListEntry
	diagnostics := 0
	for _, source := range sources {
		entry := domain.PluginListEntry{ID: source.ID, Format: source.Kind, Version: source.Version, Description: source.Description, Argument: source.Argument}
		if source.Code != "" {
			entry.Status = domain.PluginInvalid
			switch source.Code {
			case "plugin_name_conflict":
				entry.Status = domain.PluginConflict
			case "plugin_root_unavailable", "plugin_path_unsafe", "plugin_storage_overlap", "plugin_root_limit", "plugin_catalog_limit", "plugin_platform_unsupported":
				entry.Status = domain.PluginUnavailable
			}
			entry.Diagnostics = []domain.PluginDiagnostic{{Code: source.Code}}
		} else {
			summary := byID[source.ID]
			entry.Status, entry.Available, entry.Omitted = summary.Status, summary.Available, summary.Omitted
			entry.Diagnostics = append([]domain.PluginDiagnostic(nil), summary.Diagnostics...)
		}
		remaining := 256 - diagnostics
		if len(entry.Diagnostics) > remaining {
			entry.Omitted += len(entry.Diagnostics) - remaining
			entry.Diagnostics = entry.Diagnostics[:remaining]
		}
		diagnostics += len(entry.Diagnostics)
		value, err := domain.NewPluginListEntry(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
