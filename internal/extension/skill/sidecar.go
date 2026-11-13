package skill

import (
	"bytes"
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension"
	"easycode/internal/extension/localfile"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type sidecarBinding struct {
	present, disabled bool
	digest            string
}

func readSidecar(ctx context.Context, handle extension.SourceHandle, kind string, limit int) (sidecarBinding, int, error) {
	if kind != "codex" && kind != "agentplugins" {
		return sidecarBinding{}, 0, nil
	}
	value, err := handle.Read(ctx, "agents/openai.yaml", limit)
	if errors.Is(err, os.ErrNotExist) {
		return sidecarBinding{}, 0, nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return sidecarBinding{}, value.ReadBytes, err
		}
		if ctx.Err() != nil {
			return sidecarBinding{}, value.ReadBytes, ctx.Err()
		}
		if errors.Is(err, localfile.LimitExceeded) {
			if limit < domain.MaxSkillDocumentBytes {
				return sidecarBinding{}, value.ReadBytes, failure("plugin_root_limit")
			}
			return sidecarBinding{}, value.ReadBytes, failure("skill_metadata_invalid")
		}
		return sidecarBinding{}, value.ReadBytes, failure("skill_metadata_unavailable")
	}
	binding := sidecarBinding{present: true, digest: domain.SkillDigest(string(value.Bytes))}
	binding.disabled, err = parseSidecar(value.Bytes)
	return binding, value.ReadBytes, err
}

func parseSidecar(data []byte) (bool, error) {
	if len(data) > domain.MaxSkillDocumentBytes || !utf8.Valid(data) {
		return false, failure("skill_metadata_invalid")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var node, trailing yaml.Node
	if decoder.Decode(&node) != nil || decoder.Decode(&trailing) != io.EOF || len(node.Content) != 1 || !validSidecarNode(node.Content[0], 0) {
		return false, failure("skill_metadata_invalid")
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return false, failure("skill_metadata_invalid")
	}
	disabled := false
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i].Value, root.Content[i+1]
		switch key {
		case "policy":
			if value.Kind != yaml.MappingNode {
				return false, failure("skill_metadata_invalid")
			}
			for j := 0; j < len(value.Content); j += 2 {
				name, v := value.Content[j].Value, value.Content[j+1]
				if name != "allow_implicit_invocation" {
					return false, failure("skill_unsupported_field")
				}
				if v.Kind != yaml.ScalarNode || v.Tag != "!!bool" || v.Value != "true" && v.Value != "false" {
					return false, failure("skill_metadata_invalid")
				}
				disabled = v.Value == "false"
			}
		case "interface":
			if value.Kind != yaml.MappingNode {
				return false, failure("skill_metadata_invalid")
			}
			for j := 0; j < len(value.Content); j += 2 {
				name, v := value.Content[j].Value, value.Content[j+1]
				limit := 2048
				switch name {
				case "short_description":
					limit = 1024
				case "display_name", "icon_small", "icon_large", "brand_color", "default_prompt":
				default:
					return false, failure("skill_unsupported_field")
				}
				if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || len(v.Value) > limit {
					return false, failure("skill_metadata_invalid")
				}
			}
		default:
			return false, failure("skill_unsupported_field")
		}
	}
	return disabled, nil
}

func validSidecarNode(node *yaml.Node, depth int) bool {
	if depth > 16 || node.Anchor != "" || node.Style&yaml.TaggedStyle != 0 || node.Kind == yaml.AliasNode || node.Tag == "!!null" || node.Tag == "!!merge" {
		return false
	}
	switch node.Kind {
	case yaml.MappingNode:
		if node.Tag != "!!map" {
			return false
		}
		seen := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return false
			}
			seen[key.Value] = true
		}
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return false
		}
	case yaml.ScalarNode:
		if node.Tag != "!!str" && node.Tag != "!!bool" && node.Tag != "!!int" && node.Tag != "!!float" {
			return false
		}
	default:
		return false
	}
	for _, child := range node.Content {
		if !validSidecarNode(child, depth+1) {
			return false
		}
	}
	return true
}
