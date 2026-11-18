package builtin

import (
	"easycode/internal/tool"
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
