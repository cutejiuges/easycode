package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ValidateJSON 拒绝重复键、尾随内容和有损 Unicode，保留合法扩展字段。
func ValidateJSON(data []byte) error {
	return validateJSONDepth(data, 0, false)
}

// ValidateBoundedJSON 在递归解析前施加大小、深度和 null 策略。
func ValidateBoundedJSON(data []byte, maxBytes, maxDepth int, rejectNull bool) error {
	if maxBytes < 1 || maxDepth < 1 || len(data) > maxBytes {
		return errors.New("JSON exceeds limit")
	}
	return validateJSONDepth(data, maxDepth, rejectNull)
}
func validateJSONDepth(data []byte, maxDepth int, rejectNull bool) error {
	_, err := scanJSONFields(data, maxDepth, rejectNull, 0)
	return err
}

// DecodeObject 解码封闭对象；required 必须出现，所有当前字段均不接受 null。
// 可空契约应分别使用 ValidateObjectFields 和 UnmarshalStrict 声明其字段策略。
func DecodeObject(data []byte, target any, required ...string) error {
	value, err := scanJSON(data, 0, false)
	if err != nil {
		return err
	}
	if err := validateObjectFields(value, required); err != nil {
		return err
	}
	for name, field := range value.members {
		if bytes.Equal(field.raw, []byte("null")) {
			return fmt.Errorf("JSON field %q must not be null", name)
		}
	}
	return unmarshalValidated(value, target, false)
}

// losslessJSONUnicode 在标准 JSON 解码前拒绝会被替换成 U+FFFD 的孤立代理项。
func losslessJSONUnicode(data []byte) bool {
	for i := 0; i < len(data); i++ {
		next := bytes.IndexByte(data[i:], '\\')
		if next < 0 {
			return true
		}
		i += next
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
	value, err := scanJSONFields(data, 0, false, 1)
	if err != nil {
		return nil, err
	}
	if err := validateObjectFields(value, nil); err != nil {
		return nil, err
	}
	members := make(map[string]json.RawMessage, len(value.members))
	for name, field := range value.members {
		members[name] = append(json.RawMessage(nil), field.raw...)
	}
	return members, nil
}
