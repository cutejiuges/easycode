package tool

import (
	"crypto/rand"
	"fmt"
	"time"

	"easycode/internal/domain"
)

// InvocationID 是 EasyCode 为一次 durable 工具调用分配的 UUIDv7 标识。
type InvocationID string

// GenerateInvocationID 创建新的 UUIDv7 invocation 标识。
func GenerateInvocationID() (InvocationID, error) {
	value, err := domain.GenerateUUIDv7(time.Now(), rand.Reader)
	return InvocationID(value), err
}

// ParseInvocationID 校验并返回 canonical UUIDv7 invocation 标识。
func ParseInvocationID(value string) (InvocationID, error) {
	if err := domain.ValidateUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid invocation ID: %w", err)
	}
	return InvocationID(value), nil
}

// Valid 判断 invocation 标识是否为 canonical UUIDv7。
func (id InvocationID) Valid() bool {
	return domain.ValidateUUIDv7(string(id)) == nil
}
