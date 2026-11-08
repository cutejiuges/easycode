// Package hook 实现有界本地工具检查配置与严格输出解析。
package hook

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/extension/localfile"
)

// Loader 只在显式 Load 时捕获配置；不持有可变全局或 watcher。
type Loader struct {
	beforeOpen func(string)
	afterRead  func()
}

func NewLoader() *Loader { return &Loader{} }

// Load 完整捕获两来源；坏来源以诊断保留并阻断全部修改能力。
func (l *Loader) Load(ctx context.Context, workspace, home string) (domain.HookSnapshot, error) {
	if l == nil || ctx == nil || !filepath.IsAbs(workspace) || !filepath.IsAbs(home) {
		return domain.HookSnapshot{}, domain.HookConfigInvalid
	}
	var declarations []domain.HookDeclaration
	var diagnostics []domain.HookDiagnostic
	for _, source := range []struct {
		kind       domain.HookSource
		path, root string
	}{
		{domain.HookSourceUser, filepath.Join(home, ".config", "easycode", "hooks.json"), home},
		{domain.HookSourceProject, filepath.Join(workspace, ".easycode", "hooks.json"), workspace},
	} {
		data, err := localfile.Read(ctx, source.path, domain.MaxHookConfigBytes, localfile.Options{RejectWritableFrom: source.root, Consistent: true, BeforeOpen: l.beforeOpen, AfterRead: l.afterRead})
		if ctx.Err() != nil {
			return domain.HookSnapshot{}, ctx.Err()
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		var ds []domain.HookDeclaration
		if err == nil {
			ds, err = decodeConfig(source.kind, data)
		}
		if err != nil {
			diagnostics = append(diagnostics, domain.HookDiagnostic{Source: source.kind, Code: domain.HookConfigInvalid})
			continue
		}
		declarations = append(declarations, ds...)
	}
	return domain.NewHookSnapshot(declarations, diagnostics)
}

type configFile struct {
	Hooks configEvents `json:"hooks"`
}
type configEvents struct {
	PreToolUse  []configGroup `json:"PreToolUse,omitempty"`
	PostToolUse []configGroup `json:"PostToolUse,omitempty"`
}
type configGroup struct {
	Matcher string          `json:"matcher"`
	Hooks   []configCommand `json:"hooks"`
}
type configCommand struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Command  string                     `json:"command"`
	Timeout  *int                       `json:"timeout,omitempty"`
	Feedback *domain.HookFeedbackPolicy `json:"feedback,omitempty"`
}

func (v *configEvents) UnmarshalJSON(b []byte) error {
	type fields configEvents
	var f fields
	if codec.DecodeObject(b, &f) != nil {
		return domain.HookConfigInvalid
	}
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return domain.HookConfigInvalid
	}
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		if raw, ok := m[event]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return domain.HookConfigInvalid
		}
	}
	*v = configEvents(f)
	return nil
}
func (v *configGroup) UnmarshalJSON(b []byte) error {
	type fields configGroup
	var f fields
	if codec.DecodeObject(b, &f, "matcher", "hooks") != nil || len(f.Hooks) == 0 {
		return domain.HookConfigInvalid
	}
	*v = configGroup(f)
	return nil
}
func (v *configCommand) UnmarshalJSON(b []byte) error {
	type fields configCommand
	var f fields
	if codec.DecodeObject(b, &f, "id", "type", "command") != nil {
		return domain.HookConfigInvalid
	}
	*v = configCommand(f)
	return nil
}

type normalizedCommand struct {
	ID       string                    `json:"id"`
	Command  string                    `json:"command"`
	Timeout  int                       `json:"timeout"`
	Feedback domain.HookFeedbackPolicy `json:"feedback"`
}
type normalizedGroup struct {
	Event   domain.HookEvent    `json:"event"`
	Matcher domain.HookMatcher  `json:"matcher"`
	Hooks   []normalizedCommand `json:"hooks"`
}

func decodeConfig(source domain.HookSource, b []byte) ([]domain.HookDeclaration, error) {
	if len(b) > domain.MaxHookConfigBytes {
		return nil, domain.HookConfigInvalid
	}
	var f configFile
	if codec.DecodeObject(b, &f, "hooks") != nil {
		return nil, domain.HookConfigInvalid
	}
	normalized := make([]normalizedGroup, 0, len(f.Hooks.PreToolUse))
	seen := make(map[string]bool)
	count := 0
	for _, phase := range []struct {
		event  domain.HookEvent
		groups []configGroup
	}{{domain.HookPreToolUse, f.Hooks.PreToolUse}, {domain.HookPostToolUse, f.Hooks.PostToolUse}} {
		for _, g := range phase.groups {
			matcher, err := domain.ParseHookMatcher(g.Matcher)
			if err != nil {
				return nil, domain.HookConfigInvalid
			}
			n := normalizedGroup{Event: phase.event, Matcher: matcher, Hooks: make([]normalizedCommand, 0, len(g.Hooks))}
			for _, h := range g.Hooks {
				count++
				timeout := 5
				if h.Timeout != nil {
					timeout = *h.Timeout
				}
				feedback := domain.HookFeedbackNone
				if h.Feedback != nil {
					if phase.event != domain.HookPostToolUse {
						return nil, domain.HookConfigInvalid
					}
					feedback = *h.Feedback
				}
				if h.Type != "command" || seen[h.ID] || count > 32 {
					return nil, domain.HookConfigInvalid
				}
				seen[h.ID] = true
				if _, err := domain.NewHookDeclaration(source, phase.event, feedback, domain.HookDigest(nil), h.ID, matcher, h.Command, timeout); err != nil {
					return nil, err
				}
				n.Hooks = append(n.Hooks, normalizedCommand{h.ID, h.Command, timeout, feedback})
			}
			normalized = append(normalized, n)
		}
	}
	canonical, err := codec.MarshalStable(normalized)
	if err != nil {
		return nil, domain.HookConfigInvalid
	}
	revision := domain.HookDigest(canonical)
	out := make([]domain.HookDeclaration, 0, count)
	for _, g := range normalized {
		for _, h := range g.Hooks {
			d, err := domain.NewHookDeclaration(source, g.Event, h.Feedback, revision, h.ID, g.Matcher, h.Command, h.Timeout)
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		}
	}
	return out, nil
}
