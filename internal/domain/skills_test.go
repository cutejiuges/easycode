package domain

import (
	"strings"
	"testing"
)

func TestSkillValuesBoundsAndCopies(t *testing.T) {
	if (SkillAttachment{}).Validate() == nil || (SkillMetadata{}).Validate() == nil || (SkillSource{}).Validate() == nil {
		t.Fatal("accepted zero")
	}
	source, _ := NewSkillSource("project", "nested", "easycode")
	a, err := NewSkillAttachment(source, "demo", SkillDigest("body"), "args", "instructions")
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewUserInput("01900000-0000-7000-8000-000000000001", " /demo args ")
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithSkillAttachment(a)
	if err != nil {
		t.Fatal(err)
	}
	a.Content = "changed"
	got, ok := input.SkillAttachment()
	if !ok || got.Content != "instructions" || input.Text() != " /demo args " || input.ByteSize() != len(input.Text())+got.ByteSize() {
		t.Fatal("input alias or budget")
	}
	huge, _ := NewUserInput(input.ID(), strings.Repeat("x", MaxUserInputBytes))
	if _, err := huge.WithSkillAttachment(got); err == nil {
		t.Fatal("input total unbounded")
	}
	metadata, _ := NewSkillMetadata(source, "demo", "description", "args", true, false)
	entries := []SkillMetadata{metadata}
	catalog, err := NewSkillCatalog(entries)
	if err != nil {
		t.Fatal(err)
	}
	entries[0].Description = "changed"
	copy := catalog.Entries()
	copy[0].Name = "changed"
	raw := catalog.CanonicalJSON()
	raw[0] = '!'
	if catalog.Validate() != nil || catalog.Entries()[0].Name != "demo" {
		t.Fatal("catalog alias")
	}
	for _, path := range []string{"../secret", "/tmp/file", "a/../b", "a\\b", ".", "a//b"} {
		if ValidateSkillResource(path) == nil {
			t.Fatal(path)
		}
	}
}
