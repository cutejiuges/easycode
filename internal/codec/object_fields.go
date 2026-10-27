package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// ValidateObjectFields 验证对象字段不重复且必需字段非 null；类型与未知字段由 typed decoder 验证。
func ValidateObjectFields(data []byte, required ...string) error {
	if err := ValidateJSON(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("JSON object is required")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return errors.New("JSON object has duplicate or invalid fields")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return errors.New("invalid JSON field")
		}
		seen[name] = true
		for _, field := range required {
			if field == name && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return errors.New("required JSON field is null")
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("invalid JSON object")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON object has trailing content")
	}
	for _, field := range required {
		if !seen[field] {
			return errors.New("required JSON field is missing")
		}
	}
	return nil
}
