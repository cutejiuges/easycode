package tool

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

const maxUUIDv7UnixMilli = int64(1<<48 - 1)

// InvocationID 是 EasyCode 为一次 durable 工具调用分配的 UUIDv7 标识。
type InvocationID string

// GenerateInvocationID 创建新的 UUIDv7 invocation 标识。
func GenerateInvocationID() (InvocationID, error) {
	return generateInvocationID(time.Now(), rand.Reader)
}

// ParseInvocationID 校验并返回 canonical UUIDv7 invocation 标识。
func ParseInvocationID(value string) (InvocationID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid invocation ID: %w", err)
	}
	return InvocationID(value), nil
}

// Valid 判断 invocation 标识是否为 canonical UUIDv7。
func (id InvocationID) Valid() bool {
	_, err := parseUUIDv7(string(id))
	return err == nil
}

func generateInvocationID(now time.Time, random io.Reader) (InvocationID, error) {
	milliseconds := now.UnixMilli()
	if milliseconds < 0 || milliseconds > maxUUIDv7UnixMilli {
		return "", errors.New("UUIDv7 timestamp is out of range")
	}
	if random == nil {
		return "", errors.New("UUIDv7 random source is required")
	}
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[6:]); err != nil {
		return "", fmt.Errorf("generate UUIDv7 randomness: %w", err)
	}
	for index := 5; index >= 0; index-- {
		raw[index] = byte(milliseconds)
		milliseconds >>= 8
	}
	raw[6] = raw[6]&0x0f | 0x70
	raw[8] = raw[8]&0x3f | 0x80
	return InvocationID(formatUUID(raw)), nil
}

func parseUUIDv7(value string) ([16]byte, error) {
	var raw [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return raw, errors.New("UUIDv7 must use canonical form")
	}
	compact := make([]byte, 0, 32)
	for index := range len(value) {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return raw, errors.New("UUIDv7 must use lowercase hexadecimal")
		}
		compact = append(compact, character)
	}
	if _, err := hex.Decode(raw[:], compact); err != nil {
		return raw, fmt.Errorf("decode UUIDv7: %w", err)
	}
	if raw[6]>>4 != 7 {
		return raw, errors.New("UUID version must be 7")
	}
	if raw[8]>>6 != 2 {
		return raw, errors.New("UUID variant must be RFC 9562")
	}
	if formatUUID(raw) != value {
		return raw, errors.New("UUIDv7 must use canonical form")
	}
	return raw, nil
}

func formatUUID(raw [16]byte) string {
	var encoded [36]byte
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded[:])
}
