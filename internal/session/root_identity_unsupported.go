//go:build !darwin && !linux

package session

import (
	"errors"
	"os"
)

// InspectRoot 在没有安全目录句柄的平台上失败关闭。
func (*Repository) InspectRoot() (os.FileInfo, error) {
	return nil, errors.New("secure session paths are unsupported on this platform")
}
