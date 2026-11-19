package domain

import (
	"strings"
	"testing"
)

func TestSessionTitleNormalizesSpacesWithoutChangingUnicode(t *testing.T) {
	const want = "e\u0301  修订 👩‍💻 @src \"notes\"\\"
	title, err := ParseSessionTitle("\u3000\u00a0" + want + " \u202f")
	if err != nil || title != want {
		t.Fatalf("ParseSessionTitle() = %q, %v", title, err)
	}
	again, err := ParseSessionTitle(string(title))
	if err != nil || again != title || title.Validate() != nil {
		t.Fatalf("normalized title is not stable: %q, %v", again, err)
	}
	if err := SessionTitle(" " + want).Validate(); err == nil {
		t.Fatal("canonical validation accepted leading whitespace")
	}
}

func TestSessionTitleUsesUTF8ByteLimit(t *testing.T) {
	exact := strings.Repeat("编", 85) + "a"
	title, err := ParseSessionTitle("\u3000" + exact + " ")
	if err != nil || len(title) != 256 {
		t.Fatalf("256-byte title = %d bytes, %v", len(title), err)
	}
	if _, err := ParseSessionTitle(exact + "a"); err == nil {
		t.Fatal("accepted a 257-byte title")
	}
	if ValidateSessionTitleText("") != nil || ValidateSessionTitleText("\u3000 ") != nil {
		t.Fatal("empty editorial buffer must pass raw text validation")
	}
	if _, err := ParseSessionTitle("\u3000 "); err == nil || (SessionTitle("")).Validate() == nil {
		t.Fatal("accepted an empty canonical title")
	}
}

func TestSessionTitleRejectsControlsBeforeTrimming(t *testing.T) {
	for _, text := range []string{"\ttitle", "title\n ", "title\x1b[0m", "a\x7fb", "a\u009bb", "a\u2028b", "title\u2029", "\u061ctitle", "a\u202eb", "title\u2069 ", "\xfftitle"} {
		if err := ValidateSessionTitleText(text); err == nil {
			t.Errorf("raw text validation accepted %q", text)
		}
		if _, err := ParseSessionTitle(text); err == nil {
			t.Errorf("title parsing accepted %q", text)
		}
	}
}
