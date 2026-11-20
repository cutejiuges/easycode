package catalog

import "golang.org/x/sys/unix"

func renameCatalogNoReplace(directory int, from, to string) error {
	return unix.RenameatxNp(directory, from, directory, to, unix.RENAME_EXCL)
}
