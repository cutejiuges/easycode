package skill

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension"
	"easycode/internal/extension/localfile"
	"errors"
	"os"
	"path"
	"sort"
)

type pluginScanBudget struct {
	enumerated, candidates, bytes, diagnostics int
	exhausted                                  bool
}

type pluginFilePair struct{ directory, main localfile.Identity }

func (b *pluginScanBudget) readLimit() int {
	return min(domain.MaxSkillDocumentBytes, (16<<20)-b.bytes)
}

func (l *Local) loadPlugins(ctx context.Context, contributions []extension.Contribution, sources []extension.PluginSource) error {
	l.namespaces = make(map[string]bool)
	for _, source := range sources {
		if source.ID != "" {
			l.namespaces[source.ID] = source.Code == "" && !source.Conflict
		}
	}
	ordered := append([]extension.Contribution(nil), contributions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Source.Directory < ordered[j].Source.Directory })
	var metadata []domain.SkillMetadata
	for _, e := range l.entries {
		if e.view.Status == "available" {
			metadata = append(metadata, e.view.Metadata)
		}
	}
	budget := &pluginScanBudget{}
	for _, contribution := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, rootCode := scanPlugin(ctx, contribution, budget)
		if err := ctx.Err(); err != nil {
			return err
		}
		resolvePluginLeaves(entries)
		summary := extension.PluginSummary{ID: contribution.Source.Directory}
		var added []domain.SkillMetadata
		for _, entry := range entries {
			if entry.view.Status == "available" {
				added = append(added, entry.view.Metadata)
			}
		}
		if rootCode == "" {
			all := append(append([]domain.SkillMetadata(nil), metadata...), added...)
			catalog, err := domain.NewSkillCatalog(all)
			if err != nil {
				rootCode = "plugin_catalog_limit"
			} else {
				metadata, l.catalog = all, catalog
			}
		}
		if rootCode != "" {
			summary.Status = domain.PluginUnavailable
			if rootCode == "plugin_unsupported_layout" {
				summary.Status = domain.PluginInvalid
			}
			addPluginDiagnostic(&summary, budget, "", rootCode)
			for i := range entries {
				entries[i].view.Status, entries[i].view.Code = "unavailable", rootCode
			}
		} else {
			for _, entry := range entries {
				if entry.view.Status == "available" {
					summary.Available++
				} else {
					addPluginDiagnostic(&summary, budget, entry.view.Name, entry.view.Code)
				}
			}
			switch {
			case summary.Available > 0 && len(summary.Diagnostics)+summary.Omitted > 0:
				summary.Status = domain.PluginPartial
			case summary.Available > 0:
				summary.Status = domain.PluginAvailable
			case len(summary.Diagnostics)+summary.Omitted > 0:
				summary.Status = domain.PluginInvalid
			default:
				summary.Status = domain.PluginEmpty
			}
		}
		l.namespaces[summary.ID] = summary.Status == domain.PluginAvailable || summary.Status == domain.PluginPartial || summary.Status == domain.PluginEmpty
		l.entries = append(l.entries, entries...)
		l.plugins = append(l.plugins, summary)
	}
	return nil
}

func addPluginDiagnostic(summary *extension.PluginSummary, budget *pluginScanBudget, name, code string) {
	if budget.diagnostics >= 256 {
		summary.Omitted++
		return
	}
	if ref, err := domain.ParseSkillReference(name); err != nil || !ref.Qualified() {
		name = ""
	}
	summary.Diagnostics = append(summary.Diagnostics, domain.PluginDiagnostic{Skill: name, Code: code})
	budget.diagnostics++
}

func scanPlugin(ctx context.Context, contribution extension.Contribution, budget *pluginScanBudget) ([]localEntry, string) {
	var entries []localEntry
	if contribution.Source.Validate() != nil || contribution.Source.Scope != "plugin" || contribution.Opener == nil || len(contribution.Roots) == 0 || len(contribution.Roots) > 8 {
		return nil, "plugin_root_unavailable"
	}
	roots := append([]string(nil), contribution.Roots...)
	sort.Strings(roots)
	pluginEnumerated, pluginCandidates := 0, 0
	seen := make(map[pluginFilePair]bool)
	for _, root := range roots {
		if ctx.Err() != nil {
			return nil, "plugin_root_unavailable"
		}
		if budget.exhausted || budget.readLimit() <= 0 || budget.candidates > 1024 || budget.enumerated > 8192 {
			return entries, "plugin_root_limit"
		}
		handle, err := contribution.Opener.Open(ctx, root)
		if errors.Is(err, os.ErrNotExist) && contribution.Default {
			continue
		}
		if err != nil {
			return entries, "plugin_root_unavailable"
		}
		main, mainErr := handle.Read(ctx, "SKILL.md", budget.readLimit())
		budget.bytes += main.ReadBytes
		if !errors.Is(mainErr, os.ErrNotExist) {
			if contribution.Source.Kind == "agentplugins" {
				_ = handle.Close()
				return entries, "plugin_unsupported_layout"
			}
			entry := scanPluginDocument(ctx, contribution, root, handle, main, mainErr, budget)
			_ = handle.Close()
			entries = appendScanned(entries, entry, seen)
			if budget.exhausted {
				return entries, "plugin_root_limit"
			}
			pluginCandidates++
			budget.candidates++
		} else {
			limit := min(1024, 2048-pluginEnumerated, 8192-budget.enumerated)
			names, listErr := handle.Entries(ctx, max(0, limit))
			_ = handle.Close()
			pluginEnumerated += len(names)
			budget.enumerated += len(names)
			if listErr != nil {
				if errors.Is(listErr, localfile.LimitExceeded) {
					return entries, "plugin_root_limit"
				}
				return entries, "plugin_root_unavailable"
			}
			sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })
			for _, item := range names {
				name := item.Name
				if item.Unsafe {
					entries = append(entries, invalidPluginEntry(contribution.Source, name, "skill_path_unsafe"))
					continue
				}
				if !item.Directory {
					continue
				}
				directory := path.Join(root, name)
				if domain.ValidateSkillResource(directory) != nil {
					continue
				}
				h, openErr := contribution.Opener.Open(ctx, directory)
				if openErr != nil {
					entries = append(entries, invalidPluginEntry(contribution.Source, name, "skill_path_unsafe"))
					continue
				}
				if budget.readLimit() <= 0 {
					_ = h.Close()
					return entries, "plugin_root_limit"
				}
				value, readErr := h.Read(ctx, "SKILL.md", budget.readLimit())
				budget.bytes += value.ReadBytes
				if errors.Is(readErr, os.ErrNotExist) {
					_ = h.Close()
					continue
				}
				entry := scanPluginDocument(ctx, contribution, directory, h, value, readErr, budget)
				_ = h.Close()
				entries = appendScanned(entries, entry, seen)
				if budget.exhausted {
					return entries, "plugin_root_limit"
				}
				pluginCandidates++
				budget.candidates++
				if pluginCandidates > 256 || budget.candidates > 1024 {
					return entries, "plugin_root_limit"
				}
			}
		}
		if pluginCandidates > 256 || budget.candidates > 1024 {
			return entries, "plugin_root_limit"
		}
	}
	return entries, ""
}

func appendScanned(entries []localEntry, entry localEntry, seen map[pluginFilePair]bool) []localEntry {
	if entry.plugin != nil {
		key := pluginFilePair{entry.plugin.directoryID, entry.plugin.documentID}
		if seen[key] {
			return entries
		}
		seen[key] = true
	}
	return append(entries, entry)
}

func scanPluginDocument(ctx context.Context, contribution extension.Contribution, directory string, handle extension.SourceHandle, main localfile.Content, readErr error, budget *pluginScanBudget) localEntry {
	entry := invalidPluginEntry(contribution.Source, path.Base(directory), "skill_read_failed")
	if readErr != nil {
		if errors.Is(readErr, localfile.LimitExceeded) {
			entry.view.Code = "skill_document_limit"
			budget.exhausted = budget.readLimit() < domain.MaxSkillDocumentBytes
		}
		return entry
	}
	doc, err := parseDocument(contribution.Source, path.Base(directory), string(main.Bytes))
	if contribution.Source.ValidateName(doc.metadata.Name) == nil {
		entry.view.Name = doc.metadata.Name
	}
	entry.digest = domain.SkillDigest(string(main.Bytes))
	binding := &pluginBinding{contribution: contribution.Clone(), directory: directory, directoryID: handle.Identity(), documentID: main.Identity}
	entry.plugin = binding
	if err == nil {
		var read int
		if budget.readLimit() <= 0 && (contribution.Source.Kind == "codex" || contribution.Source.Kind == "agentplugins") {
			budget.exhausted = true
			err = failure("plugin_root_limit")
		} else {
			binding.sidecar, read, err = readSidecar(ctx, handle, contribution.Source.Kind, budget.readLimit())
			budget.bytes += read
			if ErrorCode(err) == "plugin_root_limit" {
				budget.exhausted = true
			}
		}
	}
	if err != nil {
		entry.view.Code = ErrorCode(err)
		return entry
	}
	doc.metadata.DisableModelInvocation = doc.metadata.DisableModelInvocation || binding.sidecar.disabled
	entry.view.Metadata = doc.metadata
	entry.view.Name, entry.view.Status, entry.view.Code = doc.metadata.Name, "available", ""
	entry.revision = pluginRevision(*binding, doc.metadata, entry.digest)
	return entry
}

func invalidPluginEntry(source domain.SkillSource, leaf, code string) localEntry {
	name := ""
	if domain.ValidateSkillName(leaf) == nil {
		name = source.Directory + ":" + leaf
	}
	return localEntry{view: Entry{Name: name, Source: source, Status: "invalid", Code: code}}
}
func resolvePluginLeaves(entries []localEntry) {
	counts := make(map[string]int)
	for _, entry := range entries {
		if entry.view.Name != "" {
			counts[entry.view.Name]++
		}
	}
	for i := range entries {
		if counts[entries[i].view.Name] > 1 {
			entries[i].view.Status, entries[i].view.Code = "invalid", "plugin_skill_conflict"
		}
	}
}
