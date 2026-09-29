// Package codec 提供统一且可测试的 JSON 编解码入口。
package codec

import "github.com/bytedance/sonic"

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
