package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

const maxUUIDv7UnixMilli = int64(1<<48 - 1)

// GenerateSessionID 生成以当前 UTC 毫秒为时间字段的 UUIDv7 Session 标识。
func GenerateSessionID() (SessionID, error) {
	value, err := generateUUIDv7(time.Now(), rand.Reader)
	return SessionID(value), err
}

// GenerateThreadID 生成以当前 UTC 毫秒为时间字段的 UUIDv7 Thread 标识。
func GenerateThreadID() (ThreadID, error) {
	value, err := generateUUIDv7(time.Now(), rand.Reader)
	return ThreadID(value), err
}

// GenerateTurnID 生成以当前 UTC 毫秒为时间字段的 UUIDv7 Turn 标识。
func GenerateTurnID() (TurnID, error) {
	value, err := generateUUIDv7(time.Now(), rand.Reader)
	return TurnID(value), err
}

// ParseSessionID 校验并返回 canonical UUIDv7 Session 标识。
func ParseSessionID(value string) (SessionID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid session ID: %w", err)
	}
	return SessionID(value), nil
}

// ParseThreadID 校验并返回 canonical UUIDv7 Thread 标识。
func ParseThreadID(value string) (ThreadID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid thread ID: %w", err)
	}
	return ThreadID(value), nil
}

// ParseTurnID 校验并返回 canonical UUIDv7 Turn 标识。
func ParseTurnID(value string) (TurnID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid turn ID: %w", err)
	}
	return TurnID(value), nil
}

// Valid 判断 Session 标识是否为 canonical UUIDv7。
func (id SessionID) Valid() bool {
	_, err := parseUUIDv7(string(id))
	return err == nil
}

// Valid 判断 Thread 标识是否为 canonical UUIDv7。
func (id ThreadID) Valid() bool {
	_, err := parseUUIDv7(string(id))
	return err == nil
}

// Valid 判断 Turn 标识是否为 canonical UUIDv7。
func (id TurnID) Valid() bool {
	_, err := parseUUIDv7(string(id))
	return err == nil
}

// Time 返回 Session UUIDv7 内嵌的 UTC 毫秒时间。
func (id SessionID) Time() (time.Time, error) {
	return uuidv7Time(string(id), "session")
}

// Time 返回 Thread UUIDv7 内嵌的 UTC 毫秒时间。
func (id ThreadID) Time() (time.Time, error) {
	return uuidv7Time(string(id), "thread")
}

// Time 返回 Turn UUIDv7 内嵌的 UTC 毫秒时间。
func (id TurnID) Time() (time.Time, error) {
	return uuidv7Time(string(id), "turn")
}

func generateUUIDv7(now time.Time, random io.Reader) (string, error) {
	milliseconds := now.UnixMilli()
	if milliseconds < 0 || milliseconds > maxUUIDv7UnixMilli {
		return "", fmt.Errorf("UUIDv7 timestamp is out of range")
	}
	if random == nil {
		return "", fmt.Errorf("UUIDv7 random source is required")
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
	return formatUUID(raw), nil
}

func uuidv7Time(value string, kind string) (time.Time, error) {
	raw, err := parseUUIDv7(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s ID: %w", kind, err)
	}
	var milliseconds int64
	for index := 0; index < 6; index++ {
		milliseconds = milliseconds<<8 | int64(raw[index])
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func parseUUIDv7(value string) ([16]byte, error) {
	var raw [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return raw, fmt.Errorf("UUIDv7 must use canonical form")
	}
	compact := make([]byte, 0, 32)
	for index := range len(value) {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return raw, fmt.Errorf("UUIDv7 must use lowercase hexadecimal")
		}
		compact = append(compact, character)
	}
	if _, err := hex.Decode(raw[:], compact); err != nil {
		return raw, fmt.Errorf("decode UUIDv7: %w", err)
	}
	if raw[6]>>4 != 7 {
		return raw, fmt.Errorf("UUID version must be 7")
	}
	if raw[8]>>6 != 2 {
		return raw, fmt.Errorf("UUID variant must be RFC 9562")
	}
	if formatUUID(raw) != value {
		return raw, fmt.Errorf("UUIDv7 must use canonical form")
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
