package plugin

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension"
	"easycode/internal/extension/localfile"
	"os"
	"path"
)

type sourceOpener struct {
	owner                          *Sources
	root                           *os.File
	identity                       localfile.Identity
	rootPath, manifestPath, digest string
	roots                          []string
}

func (s *sourceOpener) Open(ctx context.Context, relative string) (extension.SourceHandle, error) {
	if ctx == nil || ctx.Err() != nil {
		if ctx != nil {
			return nil, ctx.Err()
		}
		return nil, extension.SourceError("skill_unavailable")
	}
	allowed := false
	for _, root := range s.roots {
		if relative == root || path.Dir(relative) == root {
			allowed = true
		}
	}
	if !allowed {
		return nil, extension.SourceError("skill_source_changed")
	}
	h, err := s.owner.acquire()
	if err != nil {
		return nil, err
	}
	if err = s.open(ctx, relative, h); err != nil {
		_ = h.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return h, nil
}
func (s *sourceOpener) open(ctx context.Context, relative string, h *sourceHandle) error {
	root, err := localfile.Open(ctx, s.rootPath, true, localfile.Options{BeforeOpen: s.owner.options.BeforeOpen})
	if err != nil {
		return extension.SourceError("skill_source_changed")
	}
	owned, err := h.track(root, false)
	if err != nil {
		return err
	}
	defer owned.close()
	id, err := localfile.FileIdentity(root)
	original, originalErr := localfile.FileIdentity(s.root)
	if err != nil || originalErr != nil || original != s.identity || id != s.identity || s.checkBoundary(ctx, root) != nil {
		return extension.SourceError("skill_source_changed")
	}
	manifest, err := localfile.OpenRelative(ctx, root, s.manifestPath, false, localfile.Options{BeforeOpen: s.owner.options.BeforeOpen})
	if err != nil {
		return extension.SourceError("skill_source_changed")
	}
	m, err := h.track(manifest, true)
	if err != nil {
		return err
	}
	b, readErr := localfile.ReadOpened(ctx, manifest, 64<<10, localfile.Options{Consistent: true})
	closeErr := m.close()
	if readErr != nil || closeErr != nil || domain.SkillDigest(string(b.Bytes)) != s.digest {
		return extension.SourceError("skill_source_changed")
	}
	if err := h.check(ctx); err != nil {
		return err
	}
	directory, err := localfile.OpenRelative(ctx, root, relative, true, localfile.Options{BeforeOpen: s.owner.options.BeforeOpen})
	if err != nil {
		return err
	}
	if _, err = h.track(directory, false); err != nil {
		return err
	}
	if s.checkBoundary(ctx, directory) != nil {
		return extension.SourceError("plugin_storage_overlap")
	}
	h.directory = directory
	h.identity, err = localfile.FileIdentity(directory)
	if err != nil {
		return extension.SourceError("skill_source_changed")
	}
	return h.check(ctx)
}
func (s *sourceOpener) checkBoundary(ctx context.Context, file *os.File) error {
	if s.owner.options.Boundary != nil {
		return s.owner.options.Boundary(ctx, file)
	}
	return nil
}
