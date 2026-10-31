package tui

import (
	"context"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type contextFixtureSession struct {
	fakeChatSession
	instructions string
	cancelled    domain.CompactionID
}

func (session *contextFixtureSession) QueryContext(context.Context) (protocol.CommandResult, error) {
	command, _ := protocol.NewQueryContextCommand("query")
	result, err := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
	item, _ := protocol.NewCommandResultItem(result)
	session.outputs <- item
	return result, err
}
func (session *contextFixtureSession) CompactContext(_ context.Context, instructions string) (protocol.CommandResult, error) {
	session.instructions = instructions
	command, _ := protocol.NewCompactContextCommand("compact", instructions)
	result, err := protocol.NewAcceptedCompactionResult(command, "01900000-0000-7000-8000-000000000009")
	item, _ := protocol.NewCommandResultItem(result)
	session.outputs <- item
	return result, err
}
func (session *contextFixtureSession) CancelCompaction(_ context.Context, id domain.CompactionID) (protocol.CommandResult, error) {
	session.cancelled = id
	command, _ := protocol.NewCancelCompactionCommand("cancel", id)
	return protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionCancelling)
}

func TestContextSlashParsing(t *testing.T) {
	for _, text := range []string{"/contextual", "/compactness", "/tmp/project", "ordinary /compact"} {
		if _, local, _ := parseContextCommand(text); local {
			t.Fatal("ordinary input intercepted")
		}
	}
	command, local, err := parseContextCommand(" /compact keep tests\nand files \n")
	if err != nil || !local || command.instructions != "keep tests\nand files" {
		t.Fatal("multiline instructions changed")
	}
	for _, text := range []string{"/context argument", "/context\nargument", "/compact " + strings.Repeat("x", (16<<10)+1)} {
		if _, local, err := parseContextCommand(text); !local || err == nil {
			t.Fatal("invalid local command accepted")
		}
	}
}

func TestLocalCompactionAdmissionDoesNotCreateInput(t *testing.T) {
	session := &contextFixtureSession{}
	model := NewModel(context.Background(), "test", session)
	if err := model.composer.replace(0, 0, "/compact keep tests"); err != nil {
		t.Fatal(err)
	}
	next, submit := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if submit == nil || model.pending == nil {
		t.Fatal("local command was not submitted")
	}
	submit()
	next, _ = model.projectControlItem(<-session.outputs)
	model = next.(Model)
	if session.instructions != "keep tests" || len(session.submits) != 0 || len(model.inputs) != 0 || len(model.transcript) != 0 || model.composer.text != "" || !model.activeCompactionID.Valid() {
		t.Fatal("local command polluted user input")
	}
	if len(model.composer.history.entries) != 1 {
		t.Fatal("accepted command missing local recall")
	}
	next, cancel := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)
	if cancel == nil {
		t.Fatal("missing compaction cancellation")
	}
	cancel()
	if session.cancelled != model.activeCompactionID {
		t.Fatal("cancel lost target")
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("next")})
	model = next.(Model)
	if model.composer.text != "next" {
		t.Fatal("cleanup blocked editing")
	}
}

func TestContextPanelSnapshotsAndPureRender(t *testing.T) {
	tokens := uint64(120)
	estimate := protocol.ContextEstimatePayload{Method: "byte_heuristic_v1", State: domain.TokenEstimateEstimated, Tokens: &tokens}
	payload := protocol.ContextSnapshotPayload{Scope: "committed", Revision: 3, Activity: "compacting", Model: "fixture-model", Sources: []protocol.ContextSourcePayload{
		{Kind: "provider_profile", Revision: "profile", Estimate: estimate},
		{Kind: "tool_catalog", Revision: "tools", Estimate: estimate},
		{Kind: "committed_history", Revision: "history", Estimate: estimate},
		{Kind: "current_input", Revision: "input", Estimate: estimate},
	}, Total: estimate, BudgetState: "not_enforced"}
	for _, test := range []struct {
		name          string
		width, height int
	}{{"context", 80, 24}, {"context_narrow", 24, 18}, {"context_tiny", 1, 1}, {"context_unknown", 80, 24}, {"context_window", 80, 24}} {
		model := NewModel(context.Background(), "fixture-model", &contextFixtureSession{})
		model.width, model.height = test.width, test.height
		snapshot := payload
		snapshot.Sources = append([]protocol.ContextSourcePayload(nil), payload.Sources...)
		total := uint64(480)
		snapshot.Total.Tokens = &total
		if test.name == "context_unknown" {
			snapshot.Total.State = domain.TokenEstimateUnknown
			snapshot.Total.Tokens = nil
			snapshot.Sources[2].Estimate = snapshot.Total
		}
		if test.name == "context_window" {
			snapshot.WindowConfigured = true
			snapshot.Window = 200000
			snapshot.ReservedOutput = 4096
			snapshot.SafetyMargin = 1000
			snapshot.BudgetState = "within_limit"
		}
		if err := snapshot.Validate(); err != nil {
			t.Fatal(err)
		}
		model.contextPanel = &snapshot
		before := model.composer
		got := model.View()
		if strings.Contains(got, "\x1b") {
			t.Fatal("context panel depends on color")
		}
		if model.composer.text != before.text || model.View() != got {
			t.Fatal("render changed state")
		}
		if len(strings.Split(got, "\n")) > test.height {
			t.Fatal("context panel exceeds height")
		}
		for _, line := range strings.Split(got, "\n") {
			if ansi.StringWidth(line) > test.width {
				t.Fatal("context panel exceeds width")
			}
		}
		path := filepath.Join("testdata", test.name+".golden")
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != got {
			t.Fatalf("%s snapshot mismatch:\n%s", test.name, got)
		}
	}
}

func TestContextRejectionAndStaleCompletionPreserveDraft(t *testing.T) {
	model := NewModel(context.Background(), "test", &contextFixtureSession{})
	if err := model.composer.replace(0, 0, "/compact keep failures"); err != nil {
		t.Fatal(err)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = next.(Model)
	draft := model.composer
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	command, _ := protocol.NewCompactContextCommand("compact", "keep failures")
	result, _ := protocol.NewRejectedCommandResult(command, protocol.ControlErrorSessionBusy)
	next, _ = model.projectContextResult(result)
	model = next.(Model)
	if !reflect.DeepEqual(model.composer, draft) || model.activeCompactionID != "" {
		t.Fatal("rejection lost draft or cursor")
	}
	model.activeCompactionID = "01900000-0000-7000-8000-000000000009"
	event, _ := protocol.NewCompactionFailed("01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", "01900000-0000-7000-8000-000000000008", protocol.CompactionFailedPayload{Code: protocol.CompactionFailureCancelled, Durable: true, UsageState: "unavailable"})
	next, _ = model.projectContextEvent(event)
	model = next.(Model)
	if !model.activeCompactionID.Valid() || !reflect.DeepEqual(model.composer, draft) {
		t.Fatal("stale terminal changed current operation")
	}
	model.contextPanel = &protocol.ContextSnapshotPayload{}
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.contextPanel != nil || cmd != nil || !model.activeCompactionID.Valid() {
		t.Fatal("panel escape cancelled operation")
	}
}

func TestCompactionMainAndRestoredBoundarySnapshots(t *testing.T) {
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000002")
	id := domain.CompactionID("01900000-0000-7000-8000-000000000003")
	page := protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{
		{Cursor: protocol.TranscriptCursor{Sequence: 4}, Kind: domain.TranscriptCompaction, CompactionID: id, Text: "Conversation compacted", More: true},
		{Cursor: protocol.TranscriptCursor{Sequence: 2}, Kind: domain.TranscriptAssistant, TurnID: turn, Text: "archived assistant"},
		{Cursor: protocol.TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptUser, TurnID: turn, InputID: domain.InputID(turn), Text: "archived user"},
	}}
	if err := page.Validate(); err != nil {
		t.Fatal(err)
	}
	q := &mainTestQueries{page: page}
	for _, phase := range []string{"compacting", "compact_completed", "compact_failed", "compact_resumed"} {
		model := NewModel(context.Background(), "test", &contextFixtureSession{}).WithTranscriptQueries(thread, q)
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
		switch phase {
		case "compacting":
			model.activeCompactionID = id
			model.compactionStatus = "Compacting conversation…"
			model.refreshState()
		case "compact_completed":
			model.compactionStatus = "Conversation compacted · estimated 5000 → 120 tokens · Ctrl+O for history"
		case "compact_failed":
			model.compactionStatus = "Compaction failed · compaction_cancelled"
		}
		if phase == "compact_completed" || phase == "compact_resumed" {
			model = updateModel(t, model, model.initialTranscriptQuery()())
			if strings.Contains(model.View(), "archived assistant") || !strings.Contains(model.View(), "Conversation compacted") {
				t.Fatal("main view did not collapse at checkpoint")
			}
			if len(q.page.Items) != 3 {
				t.Fatal("collapsed view deleted complete history")
			}
		}
		got := model.View()
		path := filepath.Join("testdata", phase+".golden")
		want, err := os.ReadFile(path)
		if err != nil || string(want) != got {
			t.Fatalf("%s snapshot changed: %v", phase, err)
		}
	}
}
