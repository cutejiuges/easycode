package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"testing"
)

func TestExplicitSkillPermissionRequiresDurableInput(t *testing.T) {
	source, _ := domain.NewSkillSource("project", ".", "easycode")
	attachment, _ := domain.NewSkillAttachment(source, "demo", domain.SkillDigest("body"), "", "frozen body")
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "sync failure"}[fail], func(t *testing.T) {
			journal := &startedFailureJournal{}
			if fail {
				journal.failAt = 1
				journal.poisonOnFailure = true
			}
			runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, journal)
			inputs := runtimeInputs("/demo")
			inputs[0], _ = inputs[0].WithSkillAttachment(attachment)
			if len(runtime.explicitSkills) != 0 {
				t.Fatal("queued input granted permission")
			}
			result := runtime.commitInputs(context.Background(), runtimeTurnID, 0, inputs, nil, func(protocol.Event) {})
			if fail {
				if result.committed || len(runtime.explicitSkills) != 0 {
					t.Fatal("failed Sync granted permission")
				}
			} else if !result.committed || len(runtime.explicitSkills) != 1 || runtime.explicitSkills[0] != attachment {
				t.Fatalf("committed selection missing: %v", result.err)
			}
		})
	}
	runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{})
	runtime.explicitSkills = []domain.SkillAttachment{attachment}
	if err := runtime.RunTurn(context.Background(), runtimeInputs("ordinary next turn"), func(protocol.Event) {}); err == nil {
		t.Fatal("missing terminal failure was not reported")
	}
	if len(runtime.explicitSkills) != 0 {
		t.Fatal("selection survived turn boundary")
	}
}
