package runtime

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestArtifactAuthorizationFollowsDurableResult(t *testing.T) {
	for _, source := range []tool.ArtifactStream{tool.ArtifactStdout, tool.ArtifactSearchResult} {
		t.Run(string(source), func(t *testing.T) {
			thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
			id, _ := tool.GenerateInvocationID()
			descriptor, _ := tool.NewArtifactDescriptor("0123456789abcdef0123456789abcdef", thread, id, source, 5, 5, strings.Repeat("a", 64), true)
			saving, _ := tool.NewArtifactSaving(tool.ArtifactComplete, "", descriptor)
			preview, _ := tool.NewModelPreview("a.go\n")
			var ready tool.ReadyCall
			var result tool.InvocationResult
			var err error
			if source == tool.ArtifactStdout {
				input, _ := tool.NewExecInput("printf output", "", 0)
				ready, _ = tool.NewExecReadyCall("call", input)
				inv, _ := tool.NewExecInvocation(id, ready)
				stats, _ := tool.NewExecStreamStats(5, 5)
				metadata, _ := tool.NewExecResultMetadata("", 0, true, 0, stats, tool.ExecStreamStats{}, false, false, false)
				none, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
				metadata, _ = metadata.WithOutputSaving(saving, none)
				result, err = tool.NewExecInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
			} else {
				input, _ := tool.NewGlobInput("*.go", "", 10)
				ready, _ = tool.NewGlobReadyCall("glob", input)
				inv, _ := tool.NewGlobInvocation(id, ready)
				metadata, _ := tool.NewGlobResultMetadata([]string{"a.go"}, false, 0, 1, tool.SearchComplete, tool.SearchSkipCounts{})
				result, _ = tool.NewGlobInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
				wrong, _ := tool.NewArtifactDescriptor(descriptor.ID(), thread, id, tool.ArtifactStdout, 5, 5, strings.Repeat("a", 64), true)
				wrongSaving, _ := tool.NewArtifactSaving(tool.ArtifactComplete, "", wrong)
				if _, err := result.WithSearchSaving(wrongSaving); err == nil {
					t.Fatal("search accepted command source")
				}
				result, err = result.WithSearchSaving(saving)
			}
			requireNoError(t, err)
			invocation, _ := tool.NewInvocation(id, ready)
			for _, fail := range []bool{false, true} {
				refs, _ := tool.NewArtifactReferences(thread, nil)
				journal := &fakeJournal{hook: func([]session.RecordDraft) {
					if _, exists := refs.Lookup(descriptor.ID()); exists {
						t.Error("reference authorized before Sync returned")
					}
				}}
				if fail {
					journal.failAt = 1
				}
				runtime := &Runtime{threadID: thread, journal: journal, artifactReferences: refs}
				seedRuntimeToolCall(t, runtime, runtimeTurnID, invocation, 0, 0)
				err := runtime.persistToolResult(context.Background(), runtimeTurnID, &result)
				_, exists := refs.Lookup(descriptor.ID())
				if (err != nil) != fail || exists == fail || runtime.poisoned.Load() != fail {
					t.Fatalf("artifact crossed Sync boundary: fail=%v readable=%v error=%v", fail, exists, err)
				}
			}
			wrongThread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcded")
			refs, _ := tool.NewArtifactReferences(wrongThread, nil)
			journal := &fakeJournal{}
			runtime := &Runtime{threadID: wrongThread, journal: journal, artifactReferences: refs}
			seedRuntimeToolCall(t, runtime, runtimeTurnID, invocation, 0, 0)
			if err := runtime.persistToolResult(context.Background(), runtimeTurnID, &result); err == nil || len(journal.snapshot()) != 0 {
				t.Fatal("foreign descriptor reached journal")
			}
		})
	}
}
