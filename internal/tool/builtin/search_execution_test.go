package builtin

import (
	"context"
	"easycode/internal/tool"
	"strings"
	"testing"
)

func finalizeSearchForTest(t *testing.T, execution tool.SearchExecution) tool.InvocationResult {
	t.Helper()
	saving, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
	if execution.NeedsSaving(tool.MaxSearchPreviewBytes) {
		saving, _ = tool.NewArtifactSaving(tool.ArtifactUnavailable, "storage_unavailable", tool.ArtifactDescriptor{})
	}
	result, err := execution.Finalize(tool.MaxSearchPreviewBytes, saving)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type searchTextRecorder struct {
	text   strings.Builder
	writes int
}

func (c *searchTextRecorder) WriteText(text string) error {
	c.writes++
	c.text.WriteString(text)
	return nil
}
func (*searchTextRecorder) Finish(bool, bool) tool.ArtifactSaving {
	saving, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
	return saving
}
func (*searchTextRecorder) Close() error { return nil }

func TestGrepCapturesSelectedOriginalBeforeLineAndBudgetCuts(t *testing.T) {
	root := t.TempDir()
	original := "needle " + strings.Repeat("中文", 600)
	writeSearchFile(t, root, "a.txt", original+"\nneedle omitted by limit\n")
	executor := openTestGrepExecutor(t, root)
	capture := &searchTextRecorder{}
	execution := executor.Execute(context.Background(), grepInvocationFor(t, "needle", "", "", tool.GrepOutputContent, false, 0, 0, 1), capture)
	if capture.writes != 1 || capture.text.String() != "a.txt:1:"+original+"\n" {
		t.Fatal("capture lost original or exceeded selection limit")
	}
	saving, _ := tool.NewArtifactSaving(tool.ArtifactUnavailable, "quota_exceeded", tool.ArtifactDescriptor{})
	result, err := execution.Finalize(tool.SafetyResultBytes, saving)
	if err != nil {
		t.Fatal(err)
	}
	if !execution.NeedsSaving(tool.SafetyResultBytes) || len(result.Preview().Text()) > tool.SafetyResultBytes {
		t.Fatal("missing budget clipping")
	}
	metadata := result.GrepMetadata()
	if metadata.OmittedMatches()-metadata.ModelOmittedMatches() != 1 {
		t.Fatal("model clipping changed query coverage")
	}
	for _, match := range metadata.Matches() {
		if !strings.Contains(result.Preview().Text(), tool.RenderGrepMatch(metadata.Mode(), match)+"\n") {
			t.Fatal("metadata retained hidden text")
		}
	}
	if strings.Contains(result.Preview().Text(), original) {
		t.Fatal("long original escaped preview budget")
	}
}

func TestGlobCaptureOrderAndLimitRemainDeterministic(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.txt", "b.txt", "a.txt"} {
		writeSearchFile(t, root, name, "fixture")
	}
	executor, _ := openTestGlobExecutor(t, root)
	capture := &searchTextRecorder{}
	execution := executor.Execute(context.Background(), globInvocationFor(t, "*.txt", "", 2), capture)
	result := finalizeSearchForTest(t, execution)
	if capture.text.String() != "a.txt\nb.txt\n" || result.GlobMetadata().OmittedMatches() != 1 || result.GlobMetadata().ModelOmittedMatches() != 0 {
		t.Fatal("selected capture changed ordering or coverage")
	}
}
