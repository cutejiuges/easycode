// Package plugin 读取显式本地技能包并持有其来源生命周期。
package plugin

import (
	"easycode/internal/domain"
	"strings"
	"unicode/utf8"
)

// ValidateDirectories 在任何资源创建前验证显式参数，不回显参数正文。
func ValidateDirectories(dirs []string) error {
	if len(dirs) > 32 {
		return sourceError("plugin_argument_limit")
	}
	for _, dir := range dirs {
		if dir == "" || len(dir) > 4096 || !utf8.ValidString(dir) || strings.ContainsRune(dir, 0) {
			return sourceError("plugin_argument_invalid")
		}
	}
	return nil
}

type manifest struct {
	ID, Kind, Version, Description string
	Roots                          []string
	Default                        bool
}

func (m manifest) source() domain.SkillSource {
	s, _ := domain.NewSkillSource("plugin", m.ID, m.Kind)
	return s
}
