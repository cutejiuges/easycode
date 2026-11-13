package localfile

import (
	"path"
	"strings"
	"unicode/utf8"
)

// Identity 只在文件系统边界内参与比较，不可序列化为宿主身份。
type Identity struct{ device, inode uint64 }

// Content 保留实际读取量；失败时不披露正文，但已发生的读取仍须计费。
type Content struct {
	Bytes     []byte
	Identity  Identity
	ReadBytes int
}

// Entry 仅供有界扫描分类，实际读取仍必须使用受校验的描述符。
type Entry struct {
	Name      string
	Directory bool
	Unsafe    bool
}

func validRelative(value string) bool {
	if value == "" || value == "." || len(value) > 4096 || !utf8.ValidString(value) || path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\\\x00:") {
		return false
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
