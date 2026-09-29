// Package codec 提供统一且可测试的 JSON 编解码入口。
package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bytedance/sonic"
)

const (
	// DefaultMaxCanonicalJSONBytes 是通用 canonical JSON 值的默认大小上限。
	DefaultMaxCanonicalJSONBytes = 64 << 20
)

// CanonicalJSON 保存经过校验且不可由调用方修改的 canonical JSON 字节。
type CanonicalJSON struct {
	data []byte
}

// MarshalCanonical 将强类型值编码为有界的 canonical JSON 快照。
func MarshalCanonical(value any, maxBytes int) (CanonicalJSON, error) {
	encoded, err := MarshalStable(value)
	if err != nil {
		return CanonicalJSON{}, fmt.Errorf("marshal canonical JSON: %w", err)
	}
	if err := validateJSONBytes(encoded, maxBytes); err != nil {
		return CanonicalJSON{}, err
	}
	return CanonicalJSON{data: append([]byte(nil), encoded...)}, nil
}

// ParseCanonical 从已有字节构造有界的 canonical JSON 快照。
func ParseCanonical(data []byte, maxBytes int) (CanonicalJSON, error) {
	if err := validateJSONBytes(data, maxBytes); err != nil {
		return CanonicalJSON{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return CanonicalJSON{}, fmt.Errorf("canonical JSON is invalid: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return CanonicalJSON{}, err
	}
	reencoded, err := MarshalStable(value)
	if err != nil {
		return CanonicalJSON{}, fmt.Errorf("re-encode canonical JSON: %w", err)
	}
	if !bytes.Equal(data, reencoded) {
		return CanonicalJSON{}, errors.New("JSON bytes are not canonical")
	}
	return CanonicalJSON{data: append([]byte(nil), data...)}, nil
}

// Bytes 返回 canonical JSON 的独立副本。
func (value CanonicalJSON) Bytes() []byte {
	return append([]byte(nil), value.data...)
}

// Len 返回 canonical JSON 的字节数。
func (value CanonicalJSON) Len() int {
	return len(value.data)
}

// Clone 返回与原值不共享可变字节的副本。
func (value CanonicalJSON) Clone() CanonicalJSON {
	return CanonicalJSON{data: value.Bytes()}
}

// Validate 验证值对象内部字节仍合法、canonical 且未超过给定上限。
func (value CanonicalJSON) Validate(maxBytes int) error {
	return validateJSONBytes(value.data, maxBytes)
}

// MarshalStable 生成缓存和 wire 测试可复现的 JSON 字节。
func MarshalStable(value any) ([]byte, error) {
	return sonic.ConfigStd.Marshal(value)
}

// Unmarshal 使用统一 Sonic 配置解码强类型数据。
func Unmarshal(data []byte, target any) error {
	return sonic.ConfigStd.Unmarshal(data, target)
}

// UnmarshalStrict 解码强类型对象并拒绝未知字段。
func UnmarshalStrict(data []byte, target any) error {
	return sonic.Config{
		EscapeHTML: true, SortMapKeys: true, CompactMarshaler: true,
		CopyString: true, ValidateString: true, DisallowUnknownFields: true,
		CaseSensitive: true,
	}.Froze().Unmarshal(data, target)
}

// Valid 判断输入是否为合法 JSON。
func Valid(data []byte) bool {
	return sonic.ConfigStd.Valid(data)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("canonical JSON has invalid trailing data: %w", err)
	}
	return errors.New("canonical JSON contains multiple values")
}

func validateJSONBytes(data []byte, maxBytes int) error {
	if len(data) == 0 {
		return errors.New("canonical JSON is required")
	}
	if maxBytes <= 0 {
		return errors.New("canonical JSON size limit must be positive")
	}
	if len(data) > maxBytes {
		return fmt.Errorf("canonical JSON exceeds %d bytes", maxBytes)
	}
	if !Valid(data) {
		return errors.New("canonical JSON is invalid")
	}
	return nil
}
