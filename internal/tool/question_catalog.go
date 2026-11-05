package tool

import "easycode/internal/codec"

// buildQuestionSchema 使用固定定义，动态题目与回答从不参与目录指纹。
func buildQuestionSchema() (codec.CanonicalJSON, error) {
	return codec.ParseCanonical([]byte(`{"additionalProperties":false,"properties":{"questions":{"items":{"additionalProperties":false,"properties":{"header":{"maxLength":12,"minLength":1,"type":"string"},"id":{"maxLength":64,"minLength":1,"pattern":"^[a-z][a-z0-9_]*$","type":"string"},"multi_select":{"type":"boolean"},"options":{"items":{"additionalProperties":false,"properties":{"description":{"maxLength":1024,"minLength":1,"type":"string"},"label":{"maxLength":128,"minLength":1,"type":"string"}},"required":["label","description"],"type":"object"},"maxItems":4,"minItems":2,"type":"array"},"question":{"maxLength":2048,"minLength":1,"type":"string"}},"required":["id","header","question","options","multi_select"],"type":"object"},"maxItems":4,"minItems":1,"type":"array"}},"required":["questions"],"type":"object"}`), maxCatalogBytes)
}
