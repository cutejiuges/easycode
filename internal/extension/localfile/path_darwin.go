package localfile

import "strings"

// SystemPath 只规范平台固定的系统别名，不解析项目 symlink。
func SystemPath(p string) string {
	for _, prefix := range []string{"/var", "/tmp", "/etc"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return "/private" + p
		}
	}
	return p
}
