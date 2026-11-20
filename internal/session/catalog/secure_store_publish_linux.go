package catalog

import "golang.org/x/sys/unix"

func renameCatalogNoReplace(directory int, from, to string) error {
	return unix.Renameat2(directory, from, directory, to, unix.RENAME_NOREPLACE)
}
