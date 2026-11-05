//go:build !darwin

package localfile

func SystemPath(p string) string { return p }
