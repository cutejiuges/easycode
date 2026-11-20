package domain

import (
	"strings"
	"unicode"
)

// SplitInputHead 切分 Unicode 首词，原样保留首词后的分隔符和参数。
func SplitInputHead(text string) (head, remainder string) {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	if end := strings.IndexFunc(text, unicode.IsSpace); end >= 0 {
		return text[:end], text[end:]
	}
	return text, ""
}

// IsReservedSkillSlashName 判断裸 slash 名称是否由已实现的内置命令占用。
// 同名 Skill 仍可通过美元前缀显式调用，限定插件名不受影响。
func IsReservedSkillSlashName(name string) bool {
	switch name {
	case "cache", "compact", "context", "effort", "hooks", "plugins", "rename", "skills", "tasks":
		return true
	default:
		return false
	}
}
