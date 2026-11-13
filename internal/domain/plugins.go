package domain

import (
	"fmt"
	"unicode/utf8"
)

// IsPluginKind 封闭列举已实现的 manifest 导入格式。
func IsPluginKind(kind string) bool {
	switch kind {
	case "easycode", "claude", "codex", "agentplugins":
		return true
	}
	return false
}

// PluginStatus 是最终 Skill 纳入后的展示状态。
type PluginStatus string

const (
	PluginAvailable   PluginStatus = "available"
	PluginPartial     PluginStatus = "partial"
	PluginEmpty       PluginStatus = "empty"
	PluginUnavailable PluginStatus = "unavailable"
	PluginInvalid     PluginStatus = "invalid"
	PluginConflict    PluginStatus = "conflict"
)

// PluginDiagnostic 只包含安全限定名与稳定英文错误码。
type PluginDiagnostic struct{ Skill, Code string }

func (d PluginDiagnostic) Validate() error {
	if d.Skill != "" {
		ref, err := ParseSkillReference(d.Skill)
		if err != nil || !ref.Qualified() {
			return fmt.Errorf("invalid plugin diagnostic skill")
		}
	}
	if len(d.Code) == 0 || len(d.Code) > 64 {
		return fmt.Errorf("invalid plugin diagnostic code")
	}
	for _, c := range d.Code {
		if c != '_' && (c < 'a' || c > 'z') {
			return fmt.Errorf("invalid plugin diagnostic code")
		}
	}
	return nil
}

// PluginListEntry 是无路径、无正文的冻结展示值。
type PluginListEntry struct {
	ID, Format, Version, Description string
	Status                           PluginStatus
	Argument, Available, Omitted     int
	Diagnostics                      []PluginDiagnostic
}

// NewPluginListEntry 只校验并复制内存，不取得来源资源。
func NewPluginListEntry(value PluginListEntry) (PluginListEntry, error) {
	if err := value.Validate(); err != nil {
		return PluginListEntry{}, err
	}
	return value.Clone(), nil
}
func (p PluginListEntry) Validate() error {
	if p.ID != "" && ValidateSkillName(p.ID) != nil || p.Format != "" && !IsPluginKind(p.Format) || p.Argument < 1 || p.Argument > 32 || p.Available < 0 || p.Available > MaxSkills || p.Omitted < 0 || len(p.Diagnostics) > 256 || !utf8.ValidString(p.Version) || len(p.Version) > 128 || !utf8.ValidString(p.Description) || len(p.Description) > 1024 {
		return fmt.Errorf("invalid plugin list entry")
	}
	switch p.Status {
	case PluginAvailable:
		if p.Available == 0 || len(p.Diagnostics) != 0 || p.Omitted != 0 {
			return fmt.Errorf("invalid available plugin")
		}
	case PluginPartial:
		if p.Available == 0 || len(p.Diagnostics)+p.Omitted == 0 {
			return fmt.Errorf("invalid partial plugin")
		}
	case PluginEmpty:
		if p.Available != 0 || len(p.Diagnostics)+p.Omitted != 0 {
			return fmt.Errorf("invalid empty plugin")
		}
	case PluginUnavailable, PluginInvalid, PluginConflict:
		if p.Available != 0 || len(p.Diagnostics)+p.Omitted == 0 {
			return fmt.Errorf("invalid disabled plugin")
		}
	default:
		return fmt.Errorf("invalid plugin status")
	}
	if (p.Status == PluginAvailable || p.Status == PluginPartial || p.Status == PluginEmpty) && (p.ID == "" || p.Format == "") {
		return fmt.Errorf("missing plugin identity")
	}
	for _, d := range p.Diagnostics {
		if d.Validate() != nil {
			return fmt.Errorf("invalid plugin diagnostic")
		}
		if d.Skill != "" {
			r, _ := ParseSkillReference(d.Skill)
			if r.Namespace() != p.ID {
				return fmt.Errorf("invalid plugin diagnostic association")
			}
		}
	}
	return nil
}
func (p PluginListEntry) Clone() PluginListEntry {
	p.Diagnostics = append([]PluginDiagnostic(nil), p.Diagnostics...)
	return p
}
