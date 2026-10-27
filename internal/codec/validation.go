package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// ValidateJSON 拒绝重复键、尾随内容和有损 Unicode，保留合法扩展字段。
func ValidateJSON(data []byte) error {
	if !utf8.Valid(data) || !losslessJSONUnicode(data) {
		return errors.New("invalid JSON Unicode")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("duplicate or invalid JSON field")
			}
			seen[name] = true
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

// DecodeObject 解码封闭对象；required 必须出现，所有当前字段均不接受 null。
// 可空契约应分别使用 ValidateObjectFields 和 UnmarshalStrict 声明其字段策略。
func DecodeObject(data []byte, target any, required ...string) error {
	if err := ValidateObjectFields(data, required...); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := Unmarshal(data, &fields); err != nil {
		return err
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("JSON field %q must not be null", name)
		}
	}
	return UnmarshalStrict(data, target)
}

// losslessJSONUnicode 在标准 JSON 解码前拒绝会被替换成 U+FFFD 的孤立代理项。
func losslessJSONUnicode(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

// ObjectMembers 返回经过无歧义检查的对象字段；字段业务策略由调用方声明。
func ObjectMembers(data []byte) (map[string]json.RawMessage, error) {
	if err := ValidateObjectFields(data); err != nil {
		return nil, err
	}
	var members map[string]json.RawMessage
	if err := Unmarshal(data, &members); err != nil {
		return nil, err
	}
	return members, nil
}
