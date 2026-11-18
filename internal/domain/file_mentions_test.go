package domain

import (
	"strings"
	"testing"
)

func TestFileMentionSyntaxOrderLiteralAndAtomicRejection(t *testing.T) {
	text := "检查 @./src/a.go#L2-3 和 @\"docs/a b.md\"#L5，再看 @src/a.go#L2-3 @src/a.go#L1"
	// 标点紧邻未引号路径是路径的一部分，行范围则必须是完整整数。
	text = strings.ReplaceAll(text, "#L5，再看", "#L5 再看")
	refs, err := ParseFileReferences(text)
	if err != nil || len(refs) != 3 || refs[0].Path() != "src/a.go" || refs[1].Path() != "docs/a b.md" || refs[0].Selection().StartLine() != 2 || refs[2].Selection().StartLine() != 1 {
		t.Fatalf("parse order: %#v %v", refs, err)
	}
	for _, literal := range []string{"foo@example.com @@name \\@secret", "`@inline` ```@other```", "~~~go\n@ignored\n~~~\nplain", "```go\n@ignored\n```\nplain", "/compact preserve @a", "/effort @a"} {
		refs, err := ParseFileReferences(literal)
		if err != nil || len(refs) != 0 {
			t.Fatalf("literal activated: %q %v", literal, err)
		}
	}
	for _, invalid := range []string{`@"unterminated`, `@"\ud800"`, `@"a\q"`, `@a#L0`, `@a#L3-2`, `@a#L+1`, `@a#L18446744073709551616`, `@a#other`, `@a @../bad`, `@a @b @c @d @e @f @g @h @i`, "@"} {
		if refs, err := ParseFileReferences(invalid); err == nil || refs != nil {
			t.Fatalf("invalid partial parse %q: %#v %v", invalid, refs, err)
		}
	}
	refs, err = ParseFileReferences(`@"\ud83d\ude00\u0023a"#L2`)
	if err != nil || len(refs) != 1 || refs[0].Path() != "😀#a" {
		t.Fatalf("surrogate pair: %v", err)
	}
	refs, err = ParseFileReferences("@a @b @c @d @e @f @g @h @a")
	if err != nil || len(refs) != 8 {
		t.Fatal("duplicate counted toward limit", err)
	}
	refs, err = ParseFileReferences("$effort @a")
	if err != nil || len(refs) != 1 {
		t.Fatal("reserved skill excluded", err)
	}
}

func FuzzFileReferenceRenderParse(f *testing.F) {
	for _, seed := range []string{"a.go", "a b.md", "@a", "a#b", "a`\"b", "中文/😀.go"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		ref, err := NewFileReference(path, NewWholeFileSelection())
		if err != nil {
			return
		}
		refs, err := ParseFileReferences(ref.String())
		if err != nil || len(refs) != 1 || refs[0] != ref {
			t.Fatalf("render failed %q: %v", ref.String(), err)
		}
	})
}

func FuzzFileMentionTargetBounds(f *testing.F) {
	f.Add("@\"a b\"#L1-2", uint32(4))
	f.Add("`@hidden` @a", uint32(12))
	f.Fuzz(func(t *testing.T, text string, position uint32) {
		if len(text) > MaxUserInputBytes {
			return
		}
		cursor := int(position % uint32(len(text)+1))
		target, ok := FindFileMentionTarget(text, cursor)
		if !ok {
			return
		}
		if target.Start < 0 || target.PathStart != target.Start+1 || target.PathEnd > len(text) || target.End > len(text) || cursor <= target.Start || cursor > target.PathEnd || len(target.Query) > MaxFilePathBytes {
			t.Fatal("unsafe target")
		}
	})
}
