// Package estimate 提供无状态、版本化且可复现的本地 token 粗估算法。
package estimate

import (
	"math"

	"easycode/internal/domain"
)

const (
	// MethodByteHeuristic 标识当前每四个字节约一个 token 的估算算法。
	MethodByteHeuristic = "byte_heuristic_v1"
	bytesPerToken       = uint64(4)
)

// ByteCount 将字节数向上取整为 token 数。
func ByteCount(bytes uint64) uint64 {
	quotient := bytes / bytesPerToken
	if bytes%bytesPerToken != 0 {
		return SaturatingAdd(quotient, 1)
	}
	return quotient
}

// StructuredByteCount 同时计入内容字节和调用方定义的结构 framing 字节。
func StructuredByteCount(contentBytes, framingBytes uint64) uint64 {
	return ByteCount(SaturatingAdd(contentBytes, framingBytes))
}

// Bytes 返回字节切片的本地 token 估算。
func Bytes(value []byte) domain.TokenEstimate {
	estimate, _ := domain.NewEstimatedTokenEstimate(MethodByteHeuristic, ByteCount(uint64(len(value))))
	return estimate
}

// String 返回字符串 UTF-8 字节的本地 token 估算。
func String(value string) domain.TokenEstimate {
	return Bytes([]byte(value))
}

// SaturatingAdd 对任意数量的无符号整数执行饱和加法。
func SaturatingAdd(values ...uint64) uint64 {
	var total uint64
	for _, value := range values {
		if value > math.MaxUint64-total {
			return math.MaxUint64
		}
		total += value
	}
	return total
}
