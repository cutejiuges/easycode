//go:build !darwin && !linux

package artifact

import (
	"context"
	"errors"
)

// Layout 在缺少描述符绑定实现的平台上拒绝启动存储。
type Layout struct{}

// OpenLayout 不以字符串路径检查替代目录身份验证。
func OpenLayout(context.Context, string, string, []string, []string) (*Layout, error) {
	return nil, errors.New("unsafe_storage_layout")
}

// Check 在不支持的平台上失败关闭。
func (*Layout) Check(context.Context) error { return errors.New("unsafe_storage_layout") }

// Close 不支持的平台没有已获取资源。
func (*Layout) Close() error { return nil }
