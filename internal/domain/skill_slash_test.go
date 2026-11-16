package domain

import "testing"

func TestReservedSkillSlashNames(t *testing.T) {
	for _, name := range []string{"cache", "compact", "context", "effort", "hooks", "plugins", "skills"} {
		if !IsReservedSkillSlashName(name) {
			t.Fatalf("builtin slash is not reserved: %s", name)
		}
	}
	for _, name := range []string{"", "Effort", "demo", "plugin:effort", "effort:review"} {
		if IsReservedSkillSlashName(name) {
			t.Fatalf("non-builtin skill was reserved: %s", name)
		}
	}
}
