// Package codec 提供统一且可测试的 JSON 编解码入口。
package codec

import "github.com/bytedance/sonic"

// StableJSON 使用兼容标准库的稳定配置，其中包含 map key 排序和字符串校验。
var StableJSON = sonic.ConfigStd

// MarshalStable 生成缓存和 wire 测试可复现的 JSON 字节。
func MarshalStable(value any) ([]byte, error) {
	return StableJSON.Marshal(value)
}

// Unmarshal 使用统一 Sonic 配置解码强类型数据。
func Unmarshal(data []byte, target any) error {
	return StableJSON.Unmarshal(data, target)
}

// Valid 判断输入是否为合法 JSON。
func Valid(data []byte) bool {
	return StableJSON.Valid(data)
}
