package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestPostSummarySurvivesNativeCommitRefreshAndRestore(t *testing.T) {
	const id = "018f1d8a-7b5c-7def-8abc-456789abcdef"
	declaration, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	for _, test := range []struct {
		state protocol.HookActivityState
		code  domain.HookCode
		label string
	}{
		{protocol.HookActivityChecking, domain.HookOK, "checking"},
		{protocol.HookActivityCompleted, domain.HookOK, "checks passed"},
		{protocol.HookActivityCompleted, domain.HookExitFailed, "checks failed"},
		{protocol.HookActivitySkipped, domain.HookInputLimit, "checks skipped"},
		{protocol.HookActivityState(domain.HookCancel), domain.HookPostInterrupted, "checks cancelled"},
		{protocol.HookActivityState(domain.HookUncertain), domain.HookOutcomeUncertain, "checks outcome_uncertain"},
	} {
		t.Run(test.label, func(t *testing.T) {
			item := protocol.TranscriptListItem{Cursor: protocol.TranscriptCursor{Sequence: 2}, Kind: domain.TranscriptCall, TurnID: domain.TurnID(id), DetailSequence: 7, Activity: protocol.ToolActivityPayload{InvocationID: id, Tool: protocol.ToolWrite, Summary: "a.go", Status: "success", Code: "ok"}, Preview: "original preview", PostHook: protocol.HookActivityPayload{InvocationID: id, Reference: declaration.Reference, State: test.state, Code: test.code}}
			queries := &mainTestQueries{page: protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{item}}}
			m := NewModel(context.Background(), "test", &fakeChatSession{}).WithTranscriptQueries(domain.ThreadID(id), queries)
			m = updateModel(t, m, m.initialTranscriptQuery()())
			assertSummary := func(m Model) {
				t.Helper()
				messages := m.visibleTranscript()
				if len(messages) != 1 || messages[0].hook == nil || messages[0].hook.State != test.state || messages[0].activity.Status != "success" || !strings.Contains(strings.Join(renderTranscript(messages, 80), "\n"), test.label) {
					t.Fatal("restored projection lost post summary or rewrote success")
				}
			}
			assertSummary(m)
			next, command := m.commitMainTranscript(9)
			m = next.(Model)
			m = updateModel(t, m, command())
			assertSummary(m)
		})
	}
}
