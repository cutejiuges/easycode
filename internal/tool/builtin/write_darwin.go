//go:build darwin

package builtin

import "golang.org/x/sys/unix"

func renameWriteNoReplace(parent int, from, to string) error {
	return unix.RenameatxNp(parent, from, parent, to, unix.RENAME_EXCL)
}
