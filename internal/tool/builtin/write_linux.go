//go:build linux

package builtin

import "golang.org/x/sys/unix"

func renameWriteNoReplace(parent int, from, to string) error {
	return unix.Renameat2(parent, from, parent, to, unix.RENAME_NOREPLACE)
}
