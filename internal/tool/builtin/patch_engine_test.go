package builtin

import (
	"strings"
	"testing"

	"easycode/internal/tool"
)

func testPatchPlan(t *testing.T, oldText, newText string, all bool) tool.PatchInput {
	t.Helper()
	replacement, err := tool.NewPatchReplacement(oldText, newText, all)
	if err != nil {
		t.Fatal(err)
	}
	input, err := tool.NewPatchInput("file.txt", []tool.PatchReplacement{replacement})
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func TestApplyPatchExactReplacements(t *testing.T) {
	for _, test := range []struct {
		name, base, old, next string
		all                   bool
		want, code            string
	}{
		{"single", "你好\r\n世界", "世界", "新世界", false, "你好\r\n新世界", ""},
		{"all", "old old\n", "old", "new", true, "new new\n", ""},
		{"ambiguous", "old old", "old", "new", false, "", "patch_match_ambiguous"},
		{"absent", "old", "none", "new", false, "", "patch_match_not_found"},
		{"overlap", "aaa", "aa", "x", true, "", "patch_overlapping_replacements"},
		{"binary", "old\x00", "old", "new", false, "", "invalid_text_file"},
		{"invalid_utf8", "old\xff", "old", "new", false, "", "invalid_text_file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			final, err := applyPatchPlan([]byte(test.base), testPatchPlan(t, test.old, test.next, test.all))
			if test.code != "" {
				if err == nil || patchPreparationCode(err) != test.code {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || string(final) != test.want {
				t.Fatalf("final = %q, error = %v", final, err)
			}
		})
	}
}

func TestApplyPatchLineHunks(t *testing.T) {
	for _, test := range []struct{ base, hunk, want, code string }{
		{"old\n", "-old\n+new", "new\n", ""},
		{"old", "-old\n+new", "new", ""},
		{"old\nnext\n", "-old", "next\n", ""},
		{"old", "-old", "", ""},
		{"\nnext\n", "-", "next\n", ""},
		{"oldish\n", "-old\n+new", "", "patch_match_not_found"},
		{"old\r\n", "-old\n+new", "", "patch_match_not_found"},
		{"first\nsecond\n", "-first\n+FIRST\n@@\n-second\n+SECOND", "FIRST\nSECOND\n", ""},
	} {
		input, err := tool.ParseOpenAIApplyPatchInput([]byte("*** Begin Patch\n*** Update File: file.txt\n@@\n" + test.hunk + "\n*** End Patch\n"))
		if err != nil {
			t.Fatal(err)
		}
		final, err := applyPatchPlan([]byte(test.base), input)
		if test.code != "" {
			if err == nil || patchPreparationCode(err) != test.code {
				t.Fatalf("error = %v", err)
			}
			continue
		}
		if err != nil || string(final) != test.want {
			t.Fatalf("base %q: final = %q, error = %v", test.base, final, err)
		}
	}
}

func TestApplyPatchOverlapAndNoChanges(t *testing.T) {
	a, _ := tool.NewPatchReplacement("ab", "cd", false)
	b, _ := tool.NewPatchReplacement("bc", "de", false)
	input, _ := tool.NewPatchInput("file.txt", []tool.PatchReplacement{a, b})
	if _, err := applyPatchPlan([]byte("abc"), input); patchPreparationCode(err) != "patch_overlapping_replacements" {
		t.Fatal(err)
	}
	a, _ = tool.NewPatchReplacement("a", "b", false)
	b, _ = tool.NewPatchReplacement("b", "a", false)
	input, _ = tool.NewPatchInput("file.txt", []tool.PatchReplacement{a, b})
	if final, err := applyPatchPlan([]byte("ab"), input); err != nil || string(final) != "ba" {
		t.Fatalf("final %q, error %v", final, err)
	}
	a, _ = tool.NewPatchReplacement("ab", "a", false)
	b, _ = tool.NewPatchReplacement("c", "bc", false)
	input, _ = tool.NewPatchInput("file.txt", []tool.PatchReplacement{a, b})
	if _, err := applyPatchPlan([]byte("abc"), input); patchPreparationCode(err) != "patch_no_changes" {
		t.Fatal(err)
	}
}

func TestPatchDiffStableCompleteAndBounded(t *testing.T) {
	want := "--- a/file.txt\n+++ b/file.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+三\n\\ No newline at end of file\n"
	for range 3 {
		diff, metadata, err := buildPatchDiff("file.txt", []byte("one\ntwo\n"), []byte("one\n三"), 1)
		if err != nil || diff != want || metadata.AddedLines() != 1 || metadata.RemovedLines() != 1 {
			t.Fatalf("diff %q, metadata %+v, error %v", diff, metadata, err)
		}
	}
	diff, _, err := buildPatchDiff("file.txt", nil, []byte("new\n"), 1)
	if err != nil || !strings.Contains(diff, "@@ -0,0 +1,1 @@\n+new\n") {
		t.Fatalf("diff %q, error %v", diff, err)
	}
	diff, _, err = buildPatchDiff("file.txt", []byte("a"), []byte(strings.Repeat("b", tool.MaxApprovalDiffBytes)), 1)
	if diff != "" || patchPreparationCode(err) != "patch_preview_too_large" {
		t.Fatalf("diff length %d, error %v", len(diff), err)
	}
}

func TestPatchDiffDistantChangesUseFixedContext(t *testing.T) {
	base := "a\n1\n2\n3\n4\n5\n6\n7\n8\nz\n"
	diff, _, err := buildPatchDiff("file.txt", []byte(base), []byte(strings.ReplaceAll(strings.ReplaceAll(base, "a\n", "A\n"), "z\n", "Z\n")), 2)
	if err != nil || strings.Count(diff, "@@ -") != 2 || strings.Contains(diff, " 4\n") || strings.Contains(diff, " 5\n") {
		t.Fatalf("diff %q, error %v", diff, err)
	}
}
