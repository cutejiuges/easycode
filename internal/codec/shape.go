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
	value, err := scanJSON(data, 0, false)
	if err != nil {
		return err
	}
	return unmarshalValidated(value, target, true)
}

// recordShapeValidator 单次遍历保留缺失字段，typed 错误仍按既有顺序优先返回。
type recordShapeValidator struct {
	required bool
	missing  error
}

func (v *recordShapeValidator) validate(value jsonValue, shape reflect.Type) error {
	if shape == nil {
		return fmt.Errorf("JSON target is required")
	}
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	if reflect.PointerTo(shape).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	if bytes.Equal(value.raw, []byte("null")) {
		if shape.Kind() == reflect.Slice || shape.Kind() == reflect.Map {
			return nil
		}
		return fmt.Errorf("JSON scalar or object must not be null")
	}
	switch shape.Kind() {
	case reflect.Struct:
		if value.members == nil {
			return fmt.Errorf("JSON object is required")
		}
		for i := 0; i < shape.NumField(); i++ {
			field := shape.Field(i)
			if !field.IsExported() {
				continue
			}
			if field.Anonymous && field.Tag.Get("json") == "" {
				if err := v.validate(value, field.Type); err != nil {
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
			raw, exists := value.members[name]
			optional := false
			for _, option := range tag[1:] {
				if option == "omitempty" {
					optional = true
				}
			}
			if !exists {
				if !optional && v.required && v.missing == nil {
					v.missing = fmt.Errorf("required JSON field %q is missing", name)
				}
				continue
			}
			missingBefore := v.missing
			if err := v.validate(raw, field.Type); err != nil {
				return fmt.Errorf("JSON field %q: %w", name, err)
			}
			if missingBefore == nil && v.missing != nil {
				v.missing = fmt.Errorf("JSON field %q: %w", name, v.missing)
			}
		}
	case reflect.Slice, reflect.Array:
		if value.raw[0] != '[' {
			return fmt.Errorf("JSON array is required")
		}
		for _, value := range value.elements {
			if err := v.validate(value, shape.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
