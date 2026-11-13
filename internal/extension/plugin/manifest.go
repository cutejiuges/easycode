package plugin

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/extension"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

func sourceError(code string) error { return extension.SourceError(code) }

type authorFields struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}
type manifestFields struct {
	Name        string          `json:"name"`
	Version     *string         `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Author      *authorFields   `json:"author,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Repository  string          `json:"repository,omitempty"`
	License     string          `json:"license,omitempty"`
	Keywords    []string        `json:"keywords,omitempty"`
	Skills      json.RawMessage `json:"skills,omitempty"`
	Schema      string          `json:"$schema,omitempty"`
	Extensions  json.RawMessage `json:"extensions,omitempty"`
}

func parseManifest(data []byte, kind string) (manifest, error) {
	m := manifest{Kind: kind}
	if codec.ValidateBoundedJSON(data, 64<<10, 16, false) != nil {
		return m, sourceError("plugin_manifest_invalid")
	}
	members, err := codec.ObjectMembers(data)
	if err != nil {
		return m, sourceError("plugin_manifest_invalid")
	}
	var name string
	if codec.Unmarshal(members["name"], &name) == nil && domain.ValidateSkillName(name) == nil {
		m.ID = name
	}
	if codec.ValidateBoundedJSON(data, 64<<10, 16, true) != nil {
		return m, sourceError("plugin_manifest_invalid")
	}
	if kind == "agentplugins" {
		var schema string
		if codec.Unmarshal(members["$schema"], &schema) != nil || schema != "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json" {
			return m, sourceError("plugin_manifest_unsupported")
		}
	}
	if err = validateManifestMembers(members, kind); err != nil {
		return m, err
	}
	var wire manifestFields
	if codec.DecodeObject(data, &wire, "name") != nil || m.ID == "" || !bounded(wire.Description, 1024) {
		return m, sourceError("plugin_manifest_invalid")
	}
	if wire.Version != nil {
		if *wire.Version == "" || !bounded(*wire.Version, 128) {
			return m, sourceError("plugin_manifest_invalid")
		}
		m.Version = *wire.Version
	}
	for _, value := range []string{wire.Homepage, wire.Repository, wire.License} {
		if !bounded(value, 2048) {
			return m, sourceError("plugin_manifest_invalid")
		}
	}
	if wire.Author != nil {
		for _, value := range []string{wire.Author.Name, wire.Author.Email, wire.Author.URL} {
			if !bounded(value, 2048) {
				return m, sourceError("plugin_manifest_invalid")
			}
		}
	}
	if len(wire.Keywords) > 32 {
		return m, sourceError("plugin_manifest_invalid")
	}
	for _, value := range wire.Keywords {
		if !bounded(value, 64) {
			return m, sourceError("plugin_manifest_invalid")
		}
	}
	m.Description = wire.Description
	m.Roots, m.Default, err = parseRoots(wire.Skills)
	return m, err
}

func validateManifestMembers(members map[string]json.RawMessage, kind string) error {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := members[name]
		switch name {
		case "name", "version", "description", "author", "homepage", "repository", "license", "keywords":
		case "skills":
			if kind == "agentplugins" {
				return sourceError("plugin_unsupported_field")
			}
		case "$schema":
			if kind != "agentplugins" {
				return sourceError("plugin_unsupported_field")
			}
		case "extensions":
			fields, err := codec.ObjectMembers(value)
			if err != nil || len(fields) != 0 {
				return sourceError("plugin_unsupported_contribution")
			}
			if kind != "agentplugins" {
				return sourceError("plugin_unsupported_field")
			}
		case "hooks", "mcpServers", "mcp_servers", "agents", "commands", "outputStyles", "lspServers", "apps":
			return sourceError("plugin_unsupported_contribution")
		default:
			return sourceError("plugin_unsupported_field")
		}
	}
	return nil
}
func bounded(value string, limit int) bool { return utf8.ValidString(value) && len(value) <= limit }

func parseRoots(raw json.RawMessage) ([]string, bool, error) {
	if len(raw) == 0 {
		return []string{"skills"}, true, nil
	}
	var roots []string
	var one string
	if codec.Unmarshal(raw, &one) == nil {
		roots = []string{one}
	} else if codec.Unmarshal(raw, &roots) != nil {
		return nil, false, sourceError("plugin_manifest_invalid")
	}
	if len(roots) == 0 || len(roots) > 8 {
		return nil, false, sourceError("plugin_root_limit")
	}
	for i, root := range roots {
		if !strings.HasPrefix(root, "./") || len(root) > 4098 || domain.ValidateSkillResource(root[2:]) != nil || len(strings.Split(root[2:], "/")) > 128 {
			return nil, false, sourceError("plugin_manifest_invalid")
		}
		roots[i] = root[2:]
	}
	sort.Strings(roots)
	out := roots[:0]
	for _, root := range roots {
		if len(out) == 0 || out[len(out)-1] != root {
			out = append(out, root)
		}
	}
	return out, false, nil
}

func (m manifest) revision() string {
	bytes, _ := codec.MarshalStable(struct {
		ID, Kind, Version, Description string
		Roots                          []string
		Default                        bool
	}{m.ID, m.Kind, m.Version, m.Description, m.Roots, m.Default})
	return domain.SkillDigest(string(bytes))
}
