package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// UnmarshalRecord 按持久化结构的 json 标签验证存在性，再执行严格强类型解码。
// omitempty 字段可省略；标量不可为 null。切片/map 的 null 是当前编码器的空集合表示。
// 自定义解码器与 RawMessage 拥有其分支或 opaque 字段规则，必须自行验证。
func UnmarshalRecord(data []byte, target any) error {
	if err := ValidateJSON(data); err != nil {
		return err
	}
	if err := UnmarshalStrict(data, target); err != nil {
		return err
	}
	if err := validateRecordShape(data, reflect.TypeOf(target), true); err != nil {
		return err
	}
	return nil
}

func validateRecordShape(data []byte, shape reflect.Type, required bool) error {
	if shape == nil {
		return fmt.Errorf("JSON target is required")
	}
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	if reflect.PointerTo(shape).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		if shape.Kind() == reflect.Slice || shape.Kind() == reflect.Map {
			return nil
		}
		return fmt.Errorf("JSON scalar or object must not be null")
	}
	switch shape.Kind() {
	case reflect.Struct:
		members, err := ObjectMembers(data)
		if err != nil {
			return err
		}
		for i := 0; i < shape.NumField(); i++ {
			field := shape.Field(i)
			if !field.IsExported() {
				continue
			}
			if field.Anonymous && field.Tag.Get("json") == "" {
				if err := validateRecordShape(data, field.Type, required); err != nil {
					return err
				}
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			raw, exists := members[name]
			optional := false
			for _, option := range tag[1:] {
				if option == "omitempty" {
					optional = true
				}
			}
			if !exists {
				if optional || !required {
					continue
				}
				return fmt.Errorf("required JSON field %q is missing", name)
			}
			if err := validateRecordShape(raw, field.Type, required); err != nil {
				return fmt.Errorf("JSON field %q: %w", name, err)
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := Unmarshal(data, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateRecordShape(value, shape.Elem(), required); err != nil {
				return err
			}
		}
	}
	return nil
}
