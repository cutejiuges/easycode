package codec

import (
	"bytes"
	"errors"
)

// ValidateObjectFields 验证对象字段不重复且必需字段非 null；类型与未知字段由 typed decoder 验证。
func ValidateObjectFields(data []byte, required ...string) error {
	value, err := scanJSONFields(data, 0, false, 1)
	if err != nil {
		return err
	}
	return validateObjectFields(value, required)
}

func validateObjectFields(value jsonValue, required []string) error {
	if value.members == nil {
		return errors.New("JSON object is required")
	}
	for _, field := range required {
		child, exists := value.members[field]
		if !exists {
			return errors.New("required JSON field is missing")
		}
		if bytes.Equal(child.raw, []byte("null")) {
			return errors.New("required JSON field is null")
		}
	}
	return nil
}
