package skill

import "strings"

func systemSkillPath(p string) string {
	for _, prefix := range []string{"/var", "/tmp", "/etc"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return "/private" + p
		}
	}
	return p
}
