package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestComposerSubmissionOwnershipAndRecovery(t *testing.T) {
	session := &fakeChatSession{submitErr: errors.New("private submission failure")}
	model := NewModel(context.Background(), "test", session)
	model.composer = composerWithText(t, "  original \n")
	model.composer.history.add("old")
	model.composer.moveTo(2)
	original := model.composer
	model, submit := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.state != stateSubmitting || len(model.transcript) != 0 || model.pending == nil {
		t.Fatal("input cleared before acceptance")
	}
	model, duplicate := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if duplicate != nil {
		t.Fatal("duplicate submit")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored"), Paste: true})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	model = updateModel(t, model, testAdmissionMessage(model, submit))
	if !reflect.DeepEqual(model.composer, original) || len(model.transcript) != 0 || model.interruptNext || model.cancelling {
		t.Fatal("rejection did not restore snapshot")
	}
	if strings.Contains(model.errorSummary, "private") {
		t.Fatal("error leaked private text")
	}
	session.submitErr = nil
	model, submit = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, wait := updateModelWithCommand(t, model, testAdmissionMessage(model, submit))
	if wait == nil || model.pending != nil || model.composer.text != "" || len(model.transcript) != 0 || len(model.inputs) != 1 || model.inputs[0].text != original.text {
		t.Fatal("acceptance did not transfer ownership")
	}
	model = testCommitAcceptedInput(t, model)
	failure, _ := protocol.NewTurnFailed("provider_request_failed", "request failed", false)
	model = updateModel(t, model, RuntimeEventMsg{Event: failure})
	if model.state != stateIdle || model.composer.text != "" || len(session.submits) != 2 || len(model.composer.history.entries) != 2 {
		t.Fatal("accepted failure retried or restored input")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	if model.composer.text != original.text {
		t.Fatal("accepted failed input cannot be recalled")
	}
}

func TestComposerRestoreRecallAfterRejectedAdmissionAndResize(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{submitErr: errors.New("rejected")})
	model.composer = composerWithText(t, "working draft")
	model.composer.history.add("recalled")
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	model, submit := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 32, Height: 10})
	model = updateModel(t, model, testAdmissionMessage(model, submit))
	if !model.composer.history.browsing || model.composer.width != 32 {
		t.Fatal("recall or new dimensions lost")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.composer.text != "working draft" {
		t.Fatal("saved draft lost")
	}
}

func TestComposerFocusRoutingAndResumeSeed(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	for _, text := range []string{"  first\r\n", " \n", strings.Repeat("x", protocol.MaxInputBytes+1), "last"} {
		model.composer.history.add(text)
	}
	if !reflect.DeepEqual(model.composer.history.entries, []string{"  first\n", "last"}) {
		t.Fatal("invalid recall seed")
	}

	model.composer = composerWithText(t, "draft\n中文")
	model.composer.moveTo(2)
	original := model.composer
	for _, mode := range []string{"approval", "detail", "admission"} {
		t.Run(mode, func(t *testing.T) {
			current := model
			switch mode {
			case "approval":
				current = updateModel(t, current, RuntimeEventMsg{Event: mustApprovalRequested(t)})
			case "detail":
				current.detailOpen = true
			case "admission":
				snapshot := current.composer
				current.pending = &snapshot
				current.state = stateSubmitting
			}
			for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("paste\n\x03"), Paste: true}, {Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeyTab}, {Type: tea.KeyLeft}, {Type: tea.KeyEnter}} {
				current = updateModel(t, current, key)
			}
			if !reflect.DeepEqual(original, current.composer) {
				t.Fatal("focus leaked to composer")
			}
		})
	}
}

func composerSnapshots(t *testing.T) map[string]Model {
	t.Helper()
	base := NewModel(context.Background(), "test", &fakeChatSession{})
	base.composer = composerWithText(t, "  中文 e\u0301 👨‍👩‍👧‍👦\n\tcode\x1b[31m \n")
	base.composer.moveTo(5)
	snapshots := map[string]Model{"composer_multiline": base}
	narrow := updateModel(t, base, tea.WindowSizeMsg{Width: 32, Height: 10})
	snapshots["composer_narrow"] = narrow
	tiny := updateModel(t, base, tea.WindowSizeMsg{Width: 2, Height: 1})
	snapshots["composer_tiny"] = tiny
	long := base
	long.composer = composerWithText(t, strings.Repeat("line 中\n", 15))
	long = updateModel(t, long, tea.WindowSizeMsg{Width: 32, Height: 10})
	snapshots["composer_scroll"] = long
	long.composer.moveTo(0)
	snapshots["composer_scroll_top"] = long
	long.composer = composerWithText(t, strings.Repeat("界", 50))
	long.composer.resize(32, 10)
	snapshots["composer_long_line"] = long
	base.composer.history.add("recalled\nline")
	recalled := updateModel(t, base, tea.KeyMsg{Type: tea.KeyUp})
	snapshots["composer_recalled"] = recalled
	pending, submit := updateModelWithCommand(t, base, tea.KeyMsg{Type: tea.KeyEnter})
	snapshots["composer_submitting"] = pending
	accepted := updateModel(t, pending, testAdmissionMessage(pending, submit))
	snapshots["composer_accepted"] = accepted
	rejected := updateModel(t, pending, submissionFailedMsg{err: errors.New("private")})
	snapshots["composer_rejected"] = rejected
	return snapshots
}

func TestComposerFixedSnapshots(t *testing.T) {
	for name, model := range composerSnapshots(t) {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			layout := model.composer.layout
			got := model.View()
			if got != string(want) {
				t.Fatalf("snapshot mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
			if strings.Contains(got, "\x1b") || got != model.View() || model.composer.layout != layout {
				t.Fatal("unsafe or impure rendering")
			}
		})
	}
}

// gatedComposerSession 用接收屏障固定取消与提交结果的先后关系。
type gatedComposerSession struct {
	fakeChatSession
	entered    chan struct{}
	release    chan struct{}
	reject     bool
	interrupts int
}

func (s *gatedComposerSession) Append(ctx context.Context, text string) (protocol.CommandResult, error) {
	close(s.entered)
	<-s.release
	if s.reject {
		return protocol.CommandResult{}, errors.New("admission rejected")
	}
	return s.fakeChatSession.Append(ctx, text)
}
func (s *gatedComposerSession) Interrupt() { s.interrupts++ }

func TestComposerCancellationDuringAdmissionBarrier(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			s := &gatedComposerSession{entered: make(chan struct{}), release: make(chan struct{}), fakeChatSession: fakeChatSession{events: make(chan protocol.Event, 1)}, reject: reject}
			failure, _ := protocol.NewTurnFailed("user_cancelled", "cancelled", true)
			s.events <- failure
			m := NewModel(context.Background(), "test", s)
			m.composer = composerWithText(t, "  pending\n")
			m, submit := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			result := make(chan tea.Msg, 1)
			done := make(chan struct{})
			go func() { defer close(done); result <- submit() }()
			<-s.entered
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
			if !m.interruptNext || s.interrupts != 0 {
				t.Error("cancel escaped admission barrier")
			}
			close(s.release)
			message := <-result
			<-done
			if reject {
				m = updateModel(t, m, message)
				if m.interruptNext || m.cancelling || m.composer.text != "  pending\n" || s.interrupts != 0 {
					t.Fatal("rejected admission retained cancellation")
				}
			} else {
				for range 4 {
					item := <-m.outputs
					next, command := m.projectControlItem(item)
					m = next.(Model)
					if command != nil {
						command()
					}
				}
				if s.interrupts != 1 || m.state != stateIdle || m.cancelling || m.interruptNext {
					t.Fatal("accepted cancellation did not finish")
				}
			}
		})
	}
}

func TestComposerSnapshotRestoredAfterOverlayClose(t *testing.T) {
	for _, overlay := range []string{"approval", "detail"} {
		t.Run(overlay, func(t *testing.T) {
			m := composerSnapshots(t)["composer_multiline"]
			before := m.composer
			if overlay == "approval" {
				event := writeApprovalEvent(t, false, false, false)
				m = updateModel(t, m, RuntimeEventMsg{Event: event})
				payload, err := protocol.DecodeToolApprovalRequested(event)
				if err != nil {
					t.Fatal(err)
				}
				resolved, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: payload.RequestID, InvocationID: payload.InvocationID, Fingerprint: payload.Fingerprint, Decision: protocol.ApprovalDeny})
				if err != nil {
					t.Fatal(err)
				}
				resolved.TurnID = event.TurnID
				m = updateModel(t, m, RuntimeEventMsg{Event: resolved})
			} else {
				m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
				m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			}
			if !reflect.DeepEqual(before, m.composer) {
				t.Fatal("overlay changed composer state")
			}
			want, err := os.ReadFile(filepath.Join("testdata", "composer_multiline.golden"))
			if err != nil {
				t.Fatal(err)
			}
			if m.View() != string(want) {
				t.Fatalf("overlay close snapshot: %s", m.View())
			}
		})
	}
}
