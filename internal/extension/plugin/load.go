package plugin

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension"
	"easycode/internal/extension/localfile"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type loadedRoot struct {
	file     *os.File
	identity localfile.Identity
	path     string
	manifest manifest
	selected string
	digest   string
	view     extension.PluginSource
}

// Load 显式分类所有 manifest 后才发布可扫描贡献；取消不返回部分快照。
func (s *Sources) Load(ctx context.Context, cwd string, dirs []string) (views []extension.PluginSource, contributions []extension.Contribution, resultErr error) {
	if err := ValidateDirectories(dirs); err != nil {
		return nil, nil, err
	}
	if !filepath.IsAbs(cwd) {
		return nil, nil, sourceError("plugin_argument_invalid")
	}
	s.mu.Lock()
	loaded := s.loaded
	s.loaded = true
	s.mu.Unlock()
	if loaded {
		return nil, nil, sourceError("plugin_already_loaded")
	}
	guard, err := s.acquire()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := guard.Close(); err != nil && resultErr == nil {
			views, contributions, resultErr = nil, nil, err
		}
	}()
	var roots []loadedRoot
	defer func() {
		for _, root := range roots {
			if root.file != nil {
				_ = root.file.Close()
			}
		}
	}()
	seen := make(map[localfile.Identity]bool)
	for index, dir := range dirs {
		if err := guard.check(ctx); err != nil {
			return nil, nil, err
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		r := s.loadRoot(ctx, filepath.Clean(dir), index+1)
		if r.file != nil {
			if seen[r.identity] {
				_ = r.file.Close()
				continue
			}
			seen[r.identity] = true
		}
		roots = append(roots, r)
	}
	counts := make(map[string]int)
	for _, root := range roots {
		if root.view.ID != "" {
			counts[root.view.ID]++
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].view.ID == roots[j].view.ID {
			return roots[i].view.Argument < roots[j].view.Argument
		}
		if roots[i].view.ID == "" || roots[j].view.ID == "" {
			return roots[j].view.ID == ""
		}
		return roots[i].view.ID < roots[j].view.ID
	})
	for i := range roots {
		r := &roots[i]
		if counts[r.view.ID] > 1 {
			r.view.Code, r.view.Conflict = "plugin_name_conflict", true
		}
		views = append(views, r.view)
		if r.view.Code != "" {
			continue
		}
		opener := &sourceOpener{owner: s, root: r.file, identity: r.identity, rootPath: r.path, manifestPath: r.selected, digest: r.digest, roots: append([]string(nil), r.manifest.Roots...)}
		contributions = append(contributions, extension.Contribution{Source: r.manifest.source(), Revision: r.manifest.revision(), Roots: append([]string(nil), r.manifest.Roots...), Default: r.manifest.Default, Opener: opener})
	}
	if err := guard.check(ctx); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	closing := s.closing
	if !closing {
		for i := range roots {
			if roots[i].view.Code == "" {
				s.roots = append(s.roots, roots[i].file)
				roots[i].file = nil
			}
		}
	}
	s.mu.Unlock()
	if closing {
		return nil, nil, context.Canceled
	}
	return views, contributions, nil
}
func (s *Sources) loadRoot(ctx context.Context, dir string, argument int) loadedRoot {
	r := loadedRoot{path: dir, view: extension.PluginSource{Argument: argument}}
	file, err := localfile.Open(ctx, dir, true, localfile.Options{BeforeOpen: s.options.BeforeOpen})
	if err != nil {
		r.view.Code = "plugin_root_unavailable"
		if errors.Is(err, localfile.Unsupported) {
			r.view.Code = "plugin_platform_unsupported"
		}
		return r
	}
	r.file = file
	r.identity, err = localfile.FileIdentity(file)
	if err != nil {
		r.view.Code = "plugin_path_unsafe"
		return r
	}
	if s.options.Boundary != nil && s.options.Boundary(ctx, file) != nil {
		r.view.Code = "plugin_storage_overlap"
		return r
	}
	for _, choice := range []struct{ file, kind string }{{".easycode-plugin/plugin.json", "easycode"}, {"plugin.json", "agentplugins"}, {".codex-plugin/plugin.json", "codex"}, {".claude-plugin/plugin.json", "claude"}} {
		f, err := localfile.OpenRelative(ctx, file, choice.file, false, localfile.Options{BeforeOpen: s.options.BeforeOpen})
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		r.view.Kind, r.selected = choice.kind, choice.file
		if err != nil {
			r.view.Code = "plugin_path_unsafe"
			return r
		}
		b, err := localfile.ReadOpened(ctx, f, 64<<10, localfile.Options{Consistent: true})
		_ = f.Close()
		if err != nil {
			r.view.Code = "plugin_manifest_invalid"
			return r
		}
		r.digest = domain.SkillDigest(string(b.Bytes))
		r.manifest, err = parseManifest(b.Bytes, choice.kind)
		r.view.ID, r.view.Version, r.view.Description = r.manifest.ID, r.manifest.Version, r.manifest.Description
		if err != nil {
			r.view.Code = err.Error()
		}
		return r
	}
	r.view.Code = "plugin_manifest_missing"
	return r
}
