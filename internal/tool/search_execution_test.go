package tool

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func searchExecutionForTest(t *testing.T, lines []string) SearchExecution {
	t.Helper()
	id, _ := GenerateInvocationID()
	input, _ := NewGlobInput("**", "", 1000)
	call, _ := NewGlobReadyCall("glob", input)
	invocation, _ := NewGlobInvocation(id, call)
	metadata, err := NewGlobResultMetadata(lines, false, 0, len(lines), SearchComplete, SearchSkipCounts{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewGlobInvocationResult(invocation, ResultSuccess, "ok", ModelPreview{}, metadata)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := NewSearchExecution(result, false)
	if err != nil {
		t.Fatal(err)
	}
	return execution
}

func TestSearchFinalizationKeepsBoundedVisibleHeadTail(t *testing.T) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = fmt.Sprintf("%04d-%s", i, strings.Repeat("x", 100))
	}
	execution := searchExecutionForTest(t, lines)
	saving, _ := NewArtifactSaving(ArtifactUnavailable, "quota_exceeded", ArtifactDescriptor{})
	for _, allowance := range []int{SafetyResultBytes, MaxSearchPreviewBytes} {
		first, err := execution.Finalize(allowance, saving)
		if err != nil {
			t.Fatal(err)
		}
		second, err := execution.Finalize(allowance, saving)
		if err != nil || first.Preview() != second.Preview() {
			t.Fatal("nondeterministic preview")
		}
		if !execution.NeedsSaving(allowance) || len(first.Preview().Text()) > allowance || !strings.Contains(first.Preview().Text(), "0000-") || !strings.Contains(first.Preview().Text(), "0999-") || !strings.Contains(first.Preview().Text(), "quota_exceeded") {
			t.Fatal("missing head, tail or saving disclosure")
		}
		for _, path := range first.GlobMetadata().Matches() {
			if !strings.Contains(first.Preview().Text(), path+"\n") {
				t.Fatal("metadata retained hidden body")
			}
		}
		if first.GlobMetadata().OmittedMatches()+len(first.GlobMetadata().Matches()) != len(lines) {
			t.Fatal("omission accounting lost candidates")
		}
	}
}

func TestSearchLongLineBoundaryIsExplicit(t *testing.T) {
	execution := searchExecutionForTest(t, []string{strings.Repeat("界", 501)})
	saving, _ := NewArtifactSaving(ArtifactUnavailable, "storage_unavailable", ArtifactDescriptor{})
	result, err := execution.Finalize(MaxSearchPreviewBytes, saving)
	if err != nil || !utf8.ValidString(result.Preview().Text()) || !strings.Contains(result.Preview().Text(), "[line truncated]") || !execution.NeedsSaving(MaxSearchPreviewBytes) {
		t.Fatal("long line lost disclosure")
	}
	if len(result.GlobMetadata().Matches()) != 0 {
		t.Fatal("hidden full path retained in metadata")
	}
	line := strings.Split(result.Preview().Text(), "\n")[1]
	if utf8.RuneCountInString(line) != 500 {
		t.Fatal("code point boundary changed")
	}
}
