package domain

// IsReservedSkillSlashName 判断裸 slash 名称是否由已实现的内置命令占用。
// 同名 Skill 仍可通过美元前缀显式调用，限定插件名不受影响。
func IsReservedSkillSlashName(name string) bool {
	switch name {
	case "cache", "compact", "context", "effort", "hooks", "plugins", "skills":
		return true
	default:
		return false
	}
}
