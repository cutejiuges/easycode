package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type preBrowserQueries struct {
	browserQueries
	sequence uint64
}

func (q *preBrowserQueries) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	if query.Source != protocol.TranscriptPreHooks {
		return q.browserQueries.ReadTranscriptBody(ctx, query)
	}
	if query.DetailSequence != q.sequence {
		return protocol.TranscriptBodyPage{}, protocol.TranscriptChanged
	}
	text := "saved final effective input"
	return protocol.TranscriptBodyPage{DetailSequence: q.sequence, Offset: query.Offset, Text: text, NextOffset: query.Offset + uint64(len(text)), TotalBytes: query.Offset + uint64(len(text))}, nil
}

func preUIActivity(t *testing.T, id string, kind protocol.PreInputKind, sequence uint64) protocol.HookActivityPayload {
	t.Helper()
	projection, err := protocol.NewPreInputProjection(kind, sequence, strings.Repeat("a", 64), "printf selected 界\\x1b")
	if err != nil {
		t.Fatal(err)
	}
	d := hookUIEntries(t)[0].Declaration
	state, code := protocol.HookActivityChecking, domain.HookOK
	if kind == protocol.PreInputSelected {
		state = protocol.HookActivityState(domain.HookContinue)
	}
	if kind == protocol.PreInputRejected {
		state, code = protocol.HookActivityState(domain.HookBlock), domain.HookTrustDenied
	}
	p := protocol.HookActivityPayload{Target: testHookTarget(t, id, domain.HookExec), PlanDigest: strings.Repeat("b", 64), Reference: d.Reference, State: state, Code: code, PreInput: projection}
	if p.Validate() != nil {
		t.Fatal("invalid pre UI fixture")
	}
	return p
}

func TestPreInputOpenDetailsRefreshKeepsAnchorAndRejectsLateChecking(t *testing.T) {
	m, _, _ := browserModel(t)
	queries := &preBrowserQueries{sequence: 21}
	m.browser.queries = queries
	list, _ := queries.ListTranscript(context.Background(), protocol.TranscriptListQuery{})
	item := list.Items[0]
	item.Activity.Status = "waiting"
	item.Activity.Code = ""
	item.Activity.ExitKnown = false
	old := preUIActivity(t, item.Activity.InvocationID, protocol.PreInputCandidate, 20)
	item.PreHook = &old
	item.DetailSequence = 20
	m.browser.items = detailItems([]protocol.TranscriptListItem{item})
	m.browser.selected = 0
	m.preInputs = map[string]protocol.HookActivityPayload{item.Activity.InvocationID: old}
	m.activeTurnID = item.TurnID
	m.detailOpen = true
	m.browser.detail = true
	m.browser.source = protocol.TranscriptPreHooks
	m.browser.generation = 7
	m.browser.offset = 400
	m.detailScroll = 3
	m.browser.previous = []uint64{0}
	m.toolItems = map[string]int{item.Activity.InvocationID: 0}
	m.toolClosed = make(map[string]bool)
	activity := item.Activity
	m.transcript = []transcriptMessage{{activity: &activity, hook: &old}}
	oldPage := cachedTranscriptPage{key: transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: protocol.TranscriptPreHooks, detailSequence: 20, offset: 400}, body: protocol.TranscriptBodyPage{DetailSequence: 20, Offset: 400, NextOffset: 403, TotalBytes: 403, Text: "old"}}
	stable := cachedTranscriptPage{key: transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: protocol.TranscriptInput}}
	m.browser.page = &oldPage
	m.browser.cache = []cachedTranscriptPage{oldPage, stable}
	final := preUIActivity(t, item.Activity.InvocationID, protocol.PreInputSelected, 21)
	e, err := protocol.NewHookActivity(final)
	if err != nil {
		t.Fatal(err)
	}
	e.TurnID = item.TurnID
	updated, cmd := m.projectHookEvent(e)
	m = updated.(Model)
	if cmd == nil || m.browser.generation <= 7 || m.browser.offset != 400 || m.detailScroll != 3 || m.browser.items[0].Cursor != item.Cursor || m.composer.text != "kept draft" || m.transcript[0].activity.Status != "waiting" || m.transcript[0].admitted || m.transcript[0].activity.Summary != final.PreInput.Summary() {
		t.Fatal("pre selection changed draft/anchor or claimed Running")
	}
	if len(m.browser.cache) != 1 || m.browser.cache[0].key != stable.key {
		t.Fatal("four-page cache retained old input detail or lost original")
	}
	updated, _ = m.receiveTranscriptPage(transcriptReply{generation: 7, page: oldPage})
	m = updated.(Model)
	if m.browser.page != nil {
		t.Fatal("old candidate page overwrote final selection")
	}
	updated, _ = m.receiveTranscriptPage(cmd().(transcriptReply))
	m = updated.(Model)
	if m.browser.page == nil || m.browser.page.body.DetailSequence != 21 {
		t.Fatal("final saved input detail not loaded")
	}
	for _, sequence := range []uint64{19, 20, 22} {
		stale := preUIActivity(t, item.Activity.InvocationID, protocol.PreInputCandidate, sequence)
		event, _ := protocol.NewHookActivity(stale)
		event.TurnID = item.TurnID
		generation := m.browser.generation
		updated, cmd = m.projectHookEvent(event)
		m = updated.(Model)
		if cmd != nil || m.browser.generation != generation || m.preInputs[item.Activity.InvocationID].PreInput.Kind() != protocol.PreInputSelected || m.transcript[0].hook.PreInput.Kind() != protocol.PreInputSelected {
			t.Fatal("late checking overwrote selected phase")
		}
	}
	for i := 0; i < 8; i++ {
		page := stable
		page.key.offset = uint64(i + 100)
		m.browser.remember(page)
	}
	if len(m.browser.cache) > 4 {
		t.Fatal("pre input detail expanded cache")
	}
}

func TestPreInputPhaseSnapshotsAndSafeNarrowReview(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	for _, kind := range []protocol.PreInputKind{protocol.PreInputCandidate, protocol.PreInputSelected, protocol.PreInputRejected} {
		for _, width := range []int{80, 28, 8} {
			t.Run(fmt.Sprintf("%s-%d", kind, width), func(t *testing.T) {
				p := preUIActivity(t, id, kind, 21)
				a := protocol.ToolActivityPayload{Tool: protocol.ToolShell, InvocationID: id, Summary: "printf original", Status: "waiting"}
				if kind == protocol.PreInputSelected {
					a.Summary = p.PreInput.Summary()
				}
				view := strings.Join(renderTranscript([]transcriptMessage{{activity: &a, hook: &p}}, width), "\n") + "\n"
				if strings.Contains(view, "\x1b") || strings.Contains(view, "· running") {
					t.Fatal("control text or candidate claimed execution")
				}
				path := filepath.Join("testdata", fmt.Sprintf("pre_input_%s_%d.golden", kind, width))
				if os.Getenv("EASYCODE_UPDATE_HOOK_SNAPSHOTS") == "1" {
					if err := os.WriteFile(path, []byte(view), 0600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil || string(want) != view {
					t.Fatal("pre phase snapshot changed", err)
				}
			})
		}
	}
}

func TestPreInputApprovalDisablesInsufficientViewportAndPreservesDefaultDeny(t *testing.T) {
	for _, command := range []bool{false, true} {
		for _, size := range [][2]int{{28, 10}, {8, 2}} {
			t.Run(fmt.Sprintf("%t-%dx%d", command, size[0], size[1]), func(t *testing.T) {
				session := &fakeChatSession{}
				m := NewModel(context.Background(), "test", session)
				m.composer = composerWithText(t, "keep draft")
				e := mustApprovalRequested(t)
				if command {
					e = mustExecApproval(t)
				}
				p, err := protocol.DecodeToolApprovalRequested(e)
				if err != nil {
					t.Fatal(err)
				}
				p.InputRewrite, err = protocol.NewPreInputProjection(protocol.PreInputSelected, 20, strings.Repeat("a", 64), "effective input")
				if err != nil {
					t.Fatal(err)
				}
				p.Reason += " PreToolUse input sources: project:rewrite"
				request, err := protocol.NewToolApprovalRequested(p)
				if err != nil {
					t.Fatal(err)
				}
				request.TurnID = e.TurnID
				m = updateModel(t, m, RuntimeEventMsg{Event: request})
				m = updateModel(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				selected := m.approval.selected
				for _, key := range []tea.KeyMsg{{Type: tea.KeyUp}, {Type: tea.KeyRight}, {Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune("\t\r"), Paste: true}} {
					next, cmd := m.Update(key)
					m = next.(Model)
					if cmd != nil || m.approval.submitted || len(session.approvals) != 0 || m.approval.selected != selected {
						t.Fatal("small terminal authorized incompletely shown input")
					}
				}
				m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
				if m.composer.text != "keep draft" || m.approval.selected != selected || !strings.Contains(m.View(), "PreToolUse") {
					t.Fatal("resize lost source review or draft")
				}
				next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = next.(Model)
				if cmd == nil {
					t.Fatal("default denial unavailable after resize")
				}
				cmd()
				if len(session.approvals) != 1 || session.approvals[0].decision != protocol.ApprovalDeny {
					t.Fatal("resize implicitly approved effective input")
				}
			})
		}
	}
}
