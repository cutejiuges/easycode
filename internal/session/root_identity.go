//go:build darwin || linux

package session

import "os"

// InspectRoot 读取 Repository 实际打开的私有根身份，不重新按路径打开。
func (repository *Repository) InspectRoot() (os.FileInfo, error) {
	root, err := repository.openedRoot()
	if err != nil {
		return nil, err
	}
	return root.file.Stat()
}
