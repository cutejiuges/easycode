package skill

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension/localfile"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Error 只暴露安全、稳定的错误码。
type Error struct{ code string }

func (e Error) Error() string   { return e.code }
func (e Error) Code() string    { return e.code }
func failure(code string) error { return Error{code} }

// ErrorCode 从来源错误获取安全分类。
func ErrorCode(e error) string {
	var value Error
	if errors.As(e, &value) {
		return value.code
	}
	if errors.Is(e, context.Canceled) {
		return "cancelled"
	}
	return "skill_read_failed"
}

// Entry 是列表展示快照，不含正文或可执行路径。
type Entry = domain.SkillListEntry

type localEntry struct {
	view         Entry
	path, digest string
}

// Local 是进程内冻结目录；每次读取重新验证实际文件描述符。
type Local struct {
	entries    []localEntry
	catalog    domain.SkillCatalog
	beforeOpen func(string)
}

// NewLocal 创建无 I/O 的本地来源。
func NewLocal() *Local { return &Local{} }
func (l *Local) Entries() []Entry {
	out := make([]Entry, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.view
	}
	return out
}
func (l *Local) Catalog() domain.SkillCatalog { return l.catalog.Clone() }

// Load 从明确的启动目录与用户目录加载一次快照。
func (l *Local) Load(ctx context.Context, cwd, home string) error {
	if l == nil || !filepath.IsAbs(cwd) || !filepath.IsAbs(home) {
		return failure("skill_root_invalid")
	}
	dirs, e := projectDirectories(ctx, cwd, l.beforeOpen)
	boundaryUnavailable := e != nil
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		dirs = nil
	}
	type root struct {
		path   string
		source domain.SkillSource
	}
	roots := make([]root, 0)
	for _, dir := range dirs {
		relative, _ := filepath.Rel(dirs[len(dirs)-1], dir)
		for _, kind := range []string{"easycode", "agents", "claude"} {
			source, _ := domain.NewSkillSource("project", filepath.ToSlash(relative), kind)
			roots = append(roots, root{filepath.Join(dir, "."+kind, "skills"), source})
		}
	}
	for _, kind := range []string{"easycode", "agents", "claude"} {
		p := filepath.Join(home, "."+kind, "skills")
		if kind == "easycode" {
			p = filepath.Join(home, ".config", "easycode", "skills")
		}
		s, _ := domain.NewSkillSource("user", ".", kind)
		roots = append(roots, root{p, s})
	}
	var entries []localEntry
	if boundaryUnavailable {
		source, _ := domain.NewSkillSource("project", ".", "easycode")
		entries = append(entries, localEntry{view: Entry{Source: source, Status: "unavailable", Code: "skill_root_unavailable"}})
	}
	seen := make(map[string]bool)
	var metadata []domain.SkillMetadata
	for _, r := range roots {
		if e := ctx.Err(); e != nil {
			return e
		}
		names, e := listDirectory(r.path, l.beforeOpen)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			entries = append(entries, localEntry{view: Entry{Source: r.source, Status: "unavailable", Code: ErrorCode(e)}})
			continue
		}
		sort.Strings(names)
		for _, name := range names {
			if domain.ValidateSkillName(name) != nil {
				continue
			}
			p := filepath.Join(r.path, name, "SKILL.md")
			text, e := readFile(ctx, p, l.beforeOpen)
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			v := Entry{Name: name, Source: r.source, Status: "available"}
			var d document
			if e == nil {
				d, e = parseDocument(r.source, name, text)
			}
			if e != nil {
				v.Status = "invalid"
				v.Code = ErrorCode(e)
			} else {
				v.Metadata = d.metadata
			}
			if seen[name] {
				v.Status = "shadowed"
			} else {
				seen[name] = true
				if e == nil {
					metadata = append(metadata, d.metadata)
				}
			}
			entries = append(entries, localEntry{v, p, domain.SkillDigest(text)})
		}
	}
	catalog, e := domain.NewSkillCatalog(metadata)
	if e != nil {
		for i := range entries {
			if entries[i].view.Status == "available" {
				entries[i].view.Status = "unavailable"
				entries[i].view.Code = "skill_catalog_limit"
			}
		}
		catalog = domain.SkillCatalog{}
	}
	l.entries = entries
	l.catalog = catalog
	return nil
}
func (l *Local) find(name string) (localEntry, error) {
	for _, e := range l.entries {
		if e.view.Name == name && e.view.Status != "shadowed" {
			if e.view.Status != "available" {
				return localEntry{}, failure("skill_unavailable")
			}
			return e, nil
		}
	}
	return localEntry{}, failure("skill_not_found")
}
func (l *Local) load(ctx context.Context, name string) (localEntry, document, error) {
	e, err := l.find(name)
	if err != nil {
		return e, document{}, err
	}
	text, err := readFile(ctx, e.path, l.beforeOpen)
	if err != nil {
		if ctx.Err() != nil {
			return e, document{}, ctx.Err()
		}
		return e, document{}, failure("skill_read_failed")
	}
	if domain.SkillDigest(text) != e.digest {
		return e, document{}, failure("skill_source_changed")
	}
	d, err := parseDocument(e.view.Source, name, text)
	return e, d, err
}

// Prepare 将显式首词调用转为独立附件，普通正文不变。
func (l *Local) Prepare(ctx context.Context, text string) (domain.SkillAttachment, bool, error) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return domain.SkillAttachment{}, false, nil
	}
	first := words[0]
	if len(first) < 2 || (first[0] != '/' && first[0] != '$') {
		return domain.SkillAttachment{}, false, nil
	}
	name := first[1:]
	switch name {
	case "skills", "context", "cache", "compact":
		if first[0] == '/' {
			return domain.SkillAttachment{}, false, nil
		}
	}
	_, err := l.find(name)
	if err != nil && first[0] == '/' {
		found := false
		for _, e := range l.entries {
			if e.view.Name == name {
				found = true
			}
		}
		if !found {
			return domain.SkillAttachment{}, false, nil
		}
	}
	e, d, err := l.load(ctx, name)
	if err != nil {
		return domain.SkillAttachment{}, false, err
	}
	if !d.metadata.UserInvocable {
		return domain.SkillAttachment{}, false, failure("skill_not_user_invocable")
	}
	args := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), first))
	a, err := expand(d, e.digest, args)
	return a, err == nil, err
}

// Read 执行模型主文档或辅助资源读取，显式资格必须由运行时从已提交事实提供。
func (l *Local) Read(ctx context.Context, name, args, resource string, explicit []domain.SkillAttachment) (domain.SkillAttachment, error) {
	e, d, err := l.load(ctx, name)
	if err != nil {
		return domain.SkillAttachment{}, err
	}
	if d.metadata.DisableModelInvocation {
		allowed := false
		if resource != "" {
			for _, a := range explicit {
				if a.Validate() == nil && a.Source == e.view.Source && a.Name == name && a.Revision == e.digest {
					allowed = true
				}
			}
		}
		if !allowed {
			return domain.SkillAttachment{}, failure("skill_model_invocation_disabled")
		}
	}
	if resource == "" {
		return expand(d, e.digest, args)
	}
	if args != "" || strings.EqualFold(resource, "SKILL.md") || domain.ValidateSkillResource(resource) != nil {
		return domain.SkillAttachment{}, failure("skill_resource_invalid")
	}
	body, err := readFile(ctx, filepath.Join(filepath.Dir(e.path), filepath.FromSlash(resource)), l.beforeOpen)
	if err != nil {
		return domain.SkillAttachment{}, failure("skill_resource_unavailable")
	}
	return domain.NewSkillAttachment(e.view.Source, name, e.digest, "", "Skill resource "+resource+" (source "+e.view.Source.ID()+")\n\n"+body)
}
func readFile(ctx context.Context, p string, hook func(string)) (string, error) {
	b, err := localfile.Read(ctx, p, domain.MaxSkillDocumentBytes, localfile.Options{BeforeOpen: hook})
	if err != nil {
		switch {
		case errors.Is(err, localfile.Unsafe):
			return "", failure("skill_path_unsafe")
		case errors.Is(err, localfile.LimitExceeded):
			return "", failure("skill_document_limit")
		case errors.Is(err, localfile.ReadFailed):
			return "", failure("skill_read_failed")
		case errors.Is(err, localfile.Unsupported):
			return "", failure("skill_platform_unsupported")
		default:
			return "", err
		}
	}
	if !utf8.Valid(b) {
		return "", failure("skill_document_invalid")
	}
	return string(b), nil
}

func listDirectory(p string, hook func(string)) ([]string, error) {
	f, e := openSecure(p, true, hook)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	names, e := f.Readdirnames(1025)
	if e != nil && !errors.Is(e, io.EOF) {
		return nil, failure("skill_read_failed")
	}
	if len(names) > 1024 {
		return nil, failure("skill_catalog_limit")
	}
	return names, nil
}
func projectDirectories(ctx context.Context, cwd string, hook func(string)) ([]string, error) {
	var dirs []string
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if len(dirs) >= 128 {
			return nil, failure("skill_catalog_limit")
		}
		dirs = append(dirs, dir)
		f, e := openSecure(filepath.Join(dir, ".git"), false, hook)
		if e == nil {
			info, err := f.Stat()
			f.Close()
			if err != nil || (!info.Mode().IsRegular() && !info.IsDir()) {
				return nil, failure("skill_path_unsafe")
			}
			return dirs, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		if filepath.Dir(dir) == dir {
			return []string{cwd}, nil
		}
	}
}
