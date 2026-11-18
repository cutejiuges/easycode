package tui

import (
	"testing"

	"easycode/internal/domain"
)

func FuzzFileCompletionSerializerPreservesRangeAndOutsideText(f *testing.F) {
	for _, path := range []string{"a.go", "a b", "@a", "a#b", "a\"b", "a`b", "目录/文.go", "a\u2028b"} {
		f.Add(path, uint8(0))
	}
	f.Fuzz(func(t *testing.T, path string, separator uint8) {
		ref, err := domain.NewFileReference(path, domain.NewWholeFileSelection())
		if err != nil {
			return
		}
		path = ref.Path()
		separators := []string{" ", " \u0301", "\t", "\n", "\u3000\ufe0f"}
		sep := separators[int(separator)%len(separators)]
		c := composerWithText(t, "prefix\n@p#L2-3"+sep+"suffix")
		c.moveTo(len("prefix\n@p"))
		target, ok := domain.FindFileMentionTarget(c.text, c.cursor)
		if !ok {
			t.Fatal("valid target missing")
		}
		if err := c.completeFile(target, path); err != nil {
			t.Fatal(err)
		}
		rendered, err := domain.RenderFilePath(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "prefix\n@" + rendered + "#L2-3" + sep + "suffix"
		if c.text != want || int(c.layout.points[c.pointIndex()].offset) != c.cursor {
			t.Fatal("completion changed outside text or split a grapheme")
		}
		refs, err := domain.ParseFileReferences(c.text)
		if err != nil || len(refs) != 1 || refs[0].Path() != path || refs[0].Selection().StartLine() != 2 || refs[0].Selection().EndLine() != 3 {
			t.Fatal("completion and submit parser disagree", err)
		}
	})
}
