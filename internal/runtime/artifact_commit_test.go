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
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdee")
	id, _ := tool.GenerateInvocationID()
	input, _ := tool.NewExecInput("printf output", "", 0)
	ready, _ := tool.NewExecReadyCall("call", input)
	invocation, _ := tool.NewExecInvocation(id, ready)
	d, _ := tool.NewArtifactDescriptor("0123456789abcdef0123456789abcdef", thread, id, tool.ArtifactStdout, 3, 3, strings.Repeat("a", 64), true)
	saving, _ := tool.NewArtifactSaving(tool.ArtifactComplete, "", d)
	none, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
	stats, _ := tool.NewExecStreamStats(3, 3)
	meta, _ := tool.NewExecResultMetadata("", 0, true, 0, stats, tool.ExecStreamStats{}, false, false, false)
	meta, _ = meta.WithOutputSaving(saving, none)
	preview, _ := tool.NewModelPreview("frozen")
	result, err := tool.NewExecInvocationResult(invocation, tool.ResultSuccess, "ok", preview, meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		refs, _ := tool.NewArtifactReferences(thread, nil)
		journal := &fakeJournal{hook: func([]session.RecordDraft) {
			if _, exists := refs.Lookup(d.ID()); exists {
				t.Error("reference authorized before Sync returned")
			}
		}}
		if fail {
			journal.failAt = 1
		}
		runtime := &Runtime{threadID: thread, journal: journal, artifactReferences: refs}
		err := runtime.persistToolResult(context.Background(), turn, result)
		_, exists := refs.Lookup(d.ID())
		if fail {
			if err == nil || exists || !runtime.poisoned.Load() {
				t.Fatal("failed durable result remained readable")
			}
		} else if err != nil || !exists {
			t.Fatalf("durable result not authorized: %v", err)
		}
	}
	wrongThread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcded")
	refs, _ := tool.NewArtifactReferences(wrongThread, nil)
	journal := &fakeJournal{}
	runtime := &Runtime{threadID: wrongThread, journal: journal, artifactReferences: refs}
	if err := runtime.persistToolResult(context.Background(), turn, result); err == nil || len(journal.snapshot()) != 0 {
		t.Fatal("foreign descriptor reached journal")
	}
}

func TestArtifactPageRequiresCommittedReferenceBeforeJournal(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdee")
	owner, _ := tool.GenerateInvocationID()
	d, err := tool.NewArtifactDescriptor("0123456789abcdef0123456789abcdef", thread, owner, tool.ArtifactStdout, 3, 3, strings.Repeat("a", 64), true)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := tool.NewReadArtifactInput(d.ID(), 0, 3)
	ready, _ := tool.NewReadArtifactReadyCall("read", input)
	id, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewReadArtifactInvocation(id, ready)
	for _, test := range []struct {
		name                         string
		authorized, complete, wantOK bool
	}{
		{"unknown", false, true, false},
		{"mismatch", true, false, false},
		{"committed", true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var descriptors []tool.ArtifactDescriptor
			if test.authorized {
				descriptors = []tool.ArtifactDescriptor{d}
			}
			refs, _ := tool.NewArtifactReferences(thread, descriptors)
			page, _ := tool.NewArtifactPage(d.ID(), 0, 3, 3, test.complete, "abc")
			preview, _ := page.Preview()
			result, err := tool.NewReadArtifactInvocationResult(invocation, tool.ResultSuccess, "ok", preview, page)
			if err != nil {
				t.Fatal(err)
			}
			journal := &fakeJournal{}
			runtime := &Runtime{threadID: thread, journal: journal, artifactReferences: refs}
			err = runtime.persistToolResult(context.Background(), turn, result)
			if test.wantOK {
				if err != nil || len(journal.snapshot()) != 1 {
					t.Fatalf("committed page rejected: %v", err)
				}
			} else if err == nil || len(journal.snapshot()) != 0 || !runtime.poisoned.Load() {
				t.Fatal("unauthorized page reached journal")
			}
		})
	}
}
