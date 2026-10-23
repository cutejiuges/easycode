package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/session/catalog"
)

func TestSessionListTextGolden(t *testing.T) {
	id, err := domain.ParseThreadID("019abcde-1234-7123-8123-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	entry := catalog.Entry{ThreadID: id, UpdatedAt: time.Date(2026, 10, 23, 8, 30, 0, 0, time.UTC), ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "example-model", CreationCWD: "/workspace/easycode"}
	for _, test := range []struct {
		name        string
		scope       catalog.ListScope
		empty, more bool
		limit       int
	}{
		{"current", catalog.ListCurrentDirectory, false, false, 20},
		{"all", catalog.ListAllDirectories, false, false, 20},
		{"empty-current", catalog.ListCurrentDirectory, true, false, 20},
		{"empty-all", catalog.ListAllDirectories, true, false, 20},
		{"limited", catalog.ListCurrentDirectory, false, true, 20},
		{"maximum", catalog.ListAllDirectories, false, true, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := catalog.ListQuery{Scope: test.scope, CreationCWD: entry.CreationCWD, Limit: test.limit}
			result := catalog.ListResult{HasMore: test.more}
			if !test.empty {
				result.Entries = []catalog.Entry{entry}
			}
			got := formatSessionList(query, result)
			want, err := os.ReadFile(filepath.Join("testdata", "session-list", test.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
	want, err := os.ReadFile(filepath.Join("testdata", "session-list", "warning.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := formatSessionListWarning(catalog.ReconciliationReport{Busy: 2, Invalid: 1}); got != string(want) {
		t.Fatalf("warning=%q", got)
	}
	if formatSessionListWarning(catalog.ReconciliationReport{}) != "" {
		t.Fatal("unexpected warning")
	}
}

func TestSessionListFieldEscapingAndBudgets(t *testing.T) {
	hostile := "\r\n\t\x00\x1b]0;title\a\x1b[31m\x7f\u0085\u2028\u2029\u061c\u200e\u200f\u202a\u202e\u2066\u2069"
	for _, limit := range []int{256, 512} {
		for _, input := range []string{hostile, strings.Repeat("界", limit+1), strings.Repeat("a", limit-2) + "\x1bxx", strings.Repeat("a", limit), "bad\xffutf8", `\n`} {
			got := sessionListField(input, limit)
			if !utf8.ValidString(got) || utf8.RuneCountInString(got) > limit {
				t.Fatalf("invalid output %q", got)
			}
			for _, r := range got {
				if unicode.IsControl(r) || strings.ContainsRune("\u2028\u2029\u061c\u200e\u200f\u202a\u202e\u2066\u2069", r) {
					t.Fatalf("unsafe %q", got)
				}
			}
		}
		if got := sessionListField(strings.Repeat("界", limit+1), limit); !strings.HasSuffix(got, "…") {
			t.Fatal("missing truncation marker")
		}
		if got := sessionListField(strings.Repeat("a", limit), limit); got != strings.Repeat("a", limit) {
			t.Fatal("exact boundary truncated")
		}
	}
	if got := sessionListField("\n\t\x1b", 256); got != `\n\t\x1b` {
		t.Fatalf("escape=%q", got)
	}
	query := catalog.ListQuery{Scope: catalog.ListCurrentDirectory, CreationCWD: hostile, Limit: 20}
	got := formatSessionList(query, catalog.ListResult{})
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\r") {
		t.Fatalf("unsafe header %q", got)
	}
}
