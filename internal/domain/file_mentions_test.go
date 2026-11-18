package domain

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestFileReferenceClosedSelectionAndPath(t *testing.T) {
	for _, path := range []string{"src/main.go", "a b/中文.md", "@special.ts", "a#b\"`.md", strings.Repeat("x", MaxFilePathBytes)} {
		ref, err := NewFileReference(path, NewWholeFileSelection())
		if err != nil {
			t.Fatal(path, err)
		}
		encoded, err := codec.MarshalStable(ref)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeFileReference(encoded)
		if err != nil || decoded != ref {
			t.Fatalf("reference round trip: %v", err)
		}
		parsed, err := ParseFileReferences(ref.String())
		if err != nil || len(parsed) != 1 || parsed[0] != ref {
			t.Fatalf("render round trip %q: %v", ref.String(), err)
		}
	}
	for _, path := range []string{"", "/etc/passwd", "C:/a", "C:a", "//host/a", "~/.key", "a/../b", "a//b", "a/./b", "a\\b", "a\x00b", "a\n", string([]byte{0xff}), strings.Repeat("x", MaxFilePathBytes+1)} {
		if _, err := NewFileReference(path, NewWholeFileSelection()); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	normalized, err := NewFileReference("./src/a", NewWholeFileSelection())
	if err != nil || normalized.Path() != "src/a" {
		t.Fatal("leading dot was not normalized")
	}
	for _, encoded := range []string{`{}`, `{"path":"./a","selection":{"kind":"whole"}}`, `{"path":"a","selection":null}`, `{"path":"a","path":"b","selection":{"kind":"whole"}}`, `{"path":"a","selection":{"kind":"whole","start_line":0}}`, `{"path":"a","selection":{"kind":"lines","start_line":1}}`, `{"path":"a","selection":{"kind":"lines","start_line":0,"end_line":1}}`, `{"path":"a","selection":{"kind":"lines","start_line":2,"end_line":1}}`, `{"path":"a","selection":{"kind":"future"}}`, `{"path":"a","selection":{"kind":"whole"},"extra":0}`} {
		if _, err := DecodeFileReference([]byte(encoded)); err == nil {
			t.Fatalf("accepted invalid reference %s", encoded)
		}
	}
	if (FileReference{}).Validate() == nil || (FileSelection{}).Validate() == nil {
		t.Fatal("zero selection accepted")
	}
	if _, err := NewLineFileSelection(math.MaxUint64, math.MaxUint64); err != nil {
		t.Fatal(err)
	}
}

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

func TestFileMentionTargetPathRangeAndQuotedPrefix(t *testing.T) {
	for _, test := range []struct {
		text   string
		cursor int
		query  string
		ok     bool
	}{
		{"检查 @src/ma#L2-5 tail", len("检查 @src/ma"), "src/ma", true},
		{"@\"docs/a b\"#L2-5 tail", len("@\"docs/a"), "docs/a", true},
		{"@\"docs/a b", len("@\"docs/a b"), "docs/a b", true},
		{"@a#L2", len("@a#L"), "", false},
		{"@a tail", 3, "", false},
		{"@", 1, "", true},
		{"@././a", len("@././a"), "a", true},
		{"@.", 2, ".", true},
		{"@@a", 3, "", false},
		{"/effort @a", 10, "", false},
		{"/compact @a", 11, "", false},
		{"text $effort @a", len("text $effort @a"), "a", true},
		{"`@a`", 3, "", false},
	} {
		target, ok := FindFileMentionTarget(test.text, test.cursor)
		if ok != test.ok || ok && target.Query != test.query {
			t.Fatalf("target %q @%d = %#v %v", test.text, test.cursor, target, ok)
		}
		if ok && (target.Start >= target.PathStart || target.PathEnd > target.End || target.End > len(test.text)) {
			t.Fatal("invalid target bounds")
		}
	}
	if _, ok := FindFileMentionTarget("@"+strings.Repeat("x", MaxFilePathBytes+1), MaxFilePathBytes+2); ok {
		t.Fatal("query was truncated")
	}
}

func TestInputDraftIntentAndReferenceCopies(t *testing.T) {
	literal, err := NewLiteralInputDraft("@../literal")
	if err != nil || literal.Interactive() || len(literal.FileReferences()) != 0 || literal.Validate() != nil {
		t.Fatal("literal activated", err)
	}
	interactive, err := NewInteractiveInputDraft("question @a @b#L1")
	if err != nil || !interactive.Interactive() || interactive.Validate() != nil {
		t.Fatal(err)
	}
	copy := interactive.FileReferences()
	copy[0] = FileReference{}
	if interactive.Validate() != nil {
		t.Fatal("getter shared a reference slice")
	}
	forged := interactive
	forged.references = copy
	if forged.Validate() == nil {
		t.Fatal("forged reference set accepted")
	}
	if reflect.DeepEqual(literal, interactive) {
		t.Fatal("intent lost")
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
