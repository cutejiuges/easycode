package domain

import (
	"easycode/internal/codec"
	"strings"
	"testing"
)

func TestSkillReferenceBoundariesAndSourceAssociation(t *testing.T) {
	for _, name := range []string{"review", "qa:review", strings.Repeat("a", 64), strings.Repeat("a", 64) + ":" + strings.Repeat("b", 64)} {
		r, err := ParseSkillReference(name)
		if err != nil || r.String() != name || r.Validate() != nil {
			t.Fatalf("reference %q: %v", name, err)
		}
	}
	for _, name := range []string{"", ":review", "qa:", "qa:a:b", "QA:review", "a--b", "qa:../x", strings.Repeat("a", 65), "qa:" + strings.Repeat("b", 65)} {
		if _, err := ParseSkillReference(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, kind := range []string{"easycode", "claude", "codex", "agentplugins"} {
		s, err := NewSkillSource("plugin", "qa", kind)
		if err != nil || s.ValidateName("qa:review") != nil || s.ValidateName("docs:review") == nil || s.ValidateName("review") == nil {
			t.Fatalf("plugin association %s: %v", kind, err)
		}
		m, err := NewSkillMetadata(s, "qa:review", "description", "", true, false)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := codec.MarshalStable(m)
		if _, err = DecodeSkillMetadata(b); err != nil {
			t.Fatal(err)
		}
		a, _ := NewSkillAttachment(s, "qa:review", SkillDigest("body"), "", "body")
		b, _ = codec.MarshalStable(a)
		for _, bad := range []string{
			strings.Replace(string(b), `"directory":"qa"`, `"directory":"docs"`, 1),
			strings.Replace(string(b), `"scope":"plugin"`, `"scope":"project"`, 1),
			strings.Replace(string(b), `"name":"qa:review"`, `"name":"review"`, 1),
			strings.Replace(string(b), `"kind":"`+kind+`"`, `"kind":"agents"`, 1),
			strings.Replace(string(b), `"scope":"plugin"`, `"scope":"plugin","extra":true`, 1),
			strings.Replace(string(b), a.Revision, strings.Repeat("Z", 64), 1),
		} {
			if _, err := DecodeSkillAttachment([]byte(bad)); err == nil {
				t.Fatalf("accepted forged attachment: %s", bad)
			}
		}
	}
	for _, kind := range []string{"easycode", "agents", "claude"} {
		s, _ := NewSkillSource("project", ".", kind)
		if s.ValidateName("review") != nil || s.ValidateName("qa:review") == nil {
			t.Fatal("local association")
		}
	}
	for _, value := range []SkillSource{{"plugin", "qa", "agents"}, {"project", ".", "codex"}, {"plugin", "../qa", "claude"}} {
		if value.Validate() == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestPluginListValuesAndClones(t *testing.T) {
	input := PluginListEntry{ID: "qa", Format: "claude", Argument: 1, Status: PluginPartial, Available: 1, Diagnostics: []PluginDiagnostic{{"qa:bad", "skill_metadata_invalid"}}}
	p, err := NewPluginListEntry(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Diagnostics[0].Code = "changed"
	cloned := p.Clone()
	cloned.Diagnostics[0].Skill = "docs:bad"
	if p.Diagnostics[0].Code != "skill_metadata_invalid" || p.Diagnostics[0].Skill != "qa:bad" {
		t.Fatal("shared diagnostics")
	}
	for _, mutate := range []func(*PluginListEntry){
		func(v *PluginListEntry) { v.Status = "mystery" },
		func(v *PluginListEntry) { v.Available = -1 },
		func(v *PluginListEntry) { v.ID = "/private/root" },
		func(v *PluginListEntry) { v.Diagnostics[0].Code = "/private/error" },
		func(v *PluginListEntry) { v.Status = PluginAvailable },
		func(v *PluginListEntry) { v.Status = PluginEmpty },
		func(v *PluginListEntry) { v.Diagnostics[0].Skill = "docs:bad" },
	} {
		v := p.Clone()
		mutate(&v)
		if _, err := NewPluginListEntry(v); err == nil {
			t.Fatal("invalid entry accepted")
		}
	}
}
