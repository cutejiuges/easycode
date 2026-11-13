package domain

import (
	"easycode/internal/codec"
	"fmt"
	"strings"
)

// SkillReference 区分本地裸名与插件限定引用，不承载文件路径。
type SkillReference struct{ namespace, leaf string }

// ParseSkillReference 严格解析唯一当前引用语法。
func ParseSkillReference(value string) (SkillReference, error) {
	namespace, leaf, qualified := strings.Cut(value, ":")
	if !qualified {
		leaf, namespace = namespace, ""
	}
	ref := SkillReference{namespace, leaf}
	if ValidateSkillName(leaf) != nil || qualified && ValidateSkillName(namespace) != nil {
		return SkillReference{}, fmt.Errorf("invalid skill reference")
	}
	return ref, nil
}

func (r SkillReference) Namespace() string { return r.namespace }
func (r SkillReference) Leaf() string      { return r.leaf }
func (r SkillReference) Qualified() bool   { return r.namespace != "" }
func (r SkillReference) String() string {
	if r.Qualified() {
		return r.namespace + ":" + r.leaf
	}
	return r.leaf
}
func (r SkillReference) Validate() error {
	_, err := ParseSkillReference(r.String())
	return err
}

// ValidateName 同时校验引用名与来源，防止伪造 namespace 关联。
func (s SkillSource) ValidateName(name string) error {
	r, err := ParseSkillReference(name)
	if err != nil || s.Validate() != nil || (s.Scope == "plugin") != r.Qualified() || r.Qualified() && r.Namespace() != s.Directory {
		return fmt.Errorf("invalid skill source association")
	}
	return nil
}

// DecodeSkillSource 对嵌套来源同样执行封闭字段校验。
func DecodeSkillSource(data []byte) (SkillSource, error) {
	type fields SkillSource
	var value fields
	if len(data) > 32<<10 || codec.DecodeObject(data, &value, "scope", "directory", "kind") != nil {
		return SkillSource{}, fmt.Errorf("invalid skill source encoding")
	}
	s := SkillSource(value)
	return s, s.Validate()
}
func (s *SkillSource) UnmarshalJSON(data []byte) error {
	v, err := DecodeSkillSource(data)
	if err == nil {
		*s = v
	}
	return err
}

// DecodeSkillMetadata 验证唯一当前模型目录结构。
func DecodeSkillMetadata(data []byte) (SkillMetadata, error) {
	type fields SkillMetadata
	var value fields
	if len(data) > MaxSkillCatalogBytes || codec.DecodeObject(data, &value, "source", "name", "description", "argument_hint", "user_invocable", "disable_model_invocation") != nil {
		return SkillMetadata{}, fmt.Errorf("invalid skill metadata encoding")
	}
	m := SkillMetadata(value)
	return m, m.Validate()
}
func (m *SkillMetadata) UnmarshalJSON(data []byte) error {
	v, err := DecodeSkillMetadata(data)
	if err == nil {
		*m = v
	}
	return err
}
