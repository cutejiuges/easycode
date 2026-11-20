package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
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

// gatedComposerSession 用接收屏障固定取消与提交结果的先后关系。
type gatedComposerSession struct {
	fakeChatSession
	entered    chan struct{}
	release    chan struct{}
	reject     bool
	interrupts int
}

func (s *gatedComposerSession) Append(ctx context.Context, draft domain.InputDraft) (protocol.CommandResult, error) {
	close(s.entered)
	<-s.release
	if s.reject {
		return protocol.CommandResult{}, errors.New("admission rejected")
	}
	return s.fakeChatSession.Append(ctx, draft)
}
func (s *gatedComposerSession) InterruptTurn(domain.TurnID) { s.interrupts++ }

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
