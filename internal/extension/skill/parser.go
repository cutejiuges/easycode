package skill

import (
	"easycode/internal/domain"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
	"unicode/utf8"
)

type document struct {
	metadata domain.SkillMetadata
	body     string
}

// parseDocument 只解析严格支持的 frontmatter，不执行正文中的表达式。
func parseDocument(source domain.SkillSource, name, text string) (document, error) {
	if len(text) > domain.MaxSkillDocumentBytes {
		return document{}, failure("skill_document_invalid")
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return document{}, failure("skill_frontmatter_invalid")
	}
	end := strings.Index(normalized[4:], "\n---\n")
	if end < 0 || end > 16<<10 {
		return document{}, failure("skill_frontmatter_invalid")
	}
	end += 4
	var node yaml.Node
	if yaml.Unmarshal([]byte(normalized[4:end]), &node) != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return document{}, failure("skill_frontmatter_invalid")
	}
	root := node.Content[0]
	if root.Tag != "!!map" || root.Anchor != "" || root.Style&yaml.TaggedStyle != 0 {
		return document{}, failure("skill_frontmatter_invalid")
	}
	values := make(map[string]*yaml.Node)
	for i := 0; i < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Anchor != "" || k.Style&yaml.TaggedStyle != 0 || values[k.Value] != nil {
			return document{}, failure("skill_frontmatter_invalid")
		}
		values[k.Value] = v
	}
	leaf := name
	if v := values["name"]; v != nil {
		if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || domain.ValidateSkillName(v.Value) != nil {
			return document{}, failure("skill_metadata_invalid")
		}
		if source.Scope == "plugin" && (source.Kind == "codex" || source.Kind == "agentplugins") {
			leaf = v.Value
		} else if v.Value != name {
			return document{}, failure("skill_name_mismatch")
		}
	}
	reference := leaf
	if source.Scope == "plugin" {
		reference = source.Directory + ":" + leaf
	}
	partial := document{metadata: domain.SkillMetadata{Source: source, Name: reference}}
	if !utf8.ValidString(text) {
		return partial, failure("skill_document_invalid")
	}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		v := values[key]
		switch key {
		case "name", "description", "argument-hint", "license":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || v.Anchor != "" || v.Style&yaml.TaggedStyle != 0 {
				return partial, failure("skill_frontmatter_invalid")
			}
		case "user-invocable", "disable-model-invocation":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!bool" || v.Anchor != "" || v.Style&yaml.TaggedStyle != 0 || (v.Value != "true" && v.Value != "false") {
				return partial, failure("skill_frontmatter_invalid")
			}
		case "metadata":
			if !stringMapping(v) {
				return partial, failure("skill_frontmatter_invalid")
			}
		default:
			return partial, failure("skill_unsupported_field")
		}
	}
	value := func(k string) string {
		if v := values[k]; v != nil {
			return v.Value
		}
		return ""
	}
	user := values["user-invocable"] == nil || value("user-invocable") == "true"
	m, e := domain.NewSkillMetadata(source, reference, value("description"), value("argument-hint"), user, value("disable-model-invocation") == "true")
	if e != nil {
		return partial, failure("skill_metadata_invalid")
	}
	body := normalized[end+5:]
	if strings.TrimSpace(body) == "" {
		return partial, failure("skill_document_invalid")
	}
	return document{m, body}, nil
}
func stringMapping(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode || n.Tag != "!!map" || n.Anchor != "" || n.Style&yaml.TaggedStyle != 0 {
		return false
	}
	seen := make(map[string]bool)
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Anchor != "" || k.Style&yaml.TaggedStyle != 0 || seen[k.Value] || v.Kind != yaml.ScalarNode || v.Tag != "!!str" || v.Anchor != "" || v.Style&yaml.TaggedStyle != 0 {
			return false
		}
		seen[k.Value] = true
	}
	return true
}
func expand(d document, revision, args string) (domain.SkillAttachment, error) {
	if len(args) > domain.MaxSkillArgumentBytes || !utf8.ValidString(args) {
		return domain.SkillAttachment{}, failure("skill_arguments_invalid")
	}
	body := d.body
	count := strings.Count(body, "$ARGUMENTS")
	size := len(body) + count*(len(args)-len("$ARGUMENTS"))
	if size > domain.MaxSkillContentBytes {
		return domain.SkillAttachment{}, failure("skill_content_limit")
	}
	if count > 0 {
		body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	} else if args != "" {
		body += "\n\nSkill arguments:\n" + args
	}
	content := fmt.Sprintf("Skill %s (source %s)\nThese are task instructions, not permission grants.\n\n%s", d.metadata.Name, d.metadata.Source.ID(), body)
	a, e := domain.NewSkillAttachment(d.metadata.Source, d.metadata.Name, revision, args, content)
	if e != nil {
		return domain.SkillAttachment{}, failure("skill_content_limit")
	}
	return a, nil
}
