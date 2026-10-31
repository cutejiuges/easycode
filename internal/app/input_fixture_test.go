package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session"
)

func (conversation *orderedConversation) EncodeUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	if err := provider.ValidateUserInputs(a, inputs); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	texts := make([]string, len(inputs))
	for i, input := range inputs {
		texts[i] = input.Text()
	}
	payload, err := json.Marshal(texts)
	if err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	return provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 3, payload)
}
func (conversation *orderedConversation) PrepareUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedUserInputs, error) {
	envelope, err := conversation.EncodeUserInputs(a, inputs)
	if err != nil {
		return nil, err
	}
	messages := make([]domain.SemanticMessage, len(inputs))
	for i, input := range inputs {
		messages[i], err = domain.NewSemanticUserMessage(input.ID(), input.Text())
		if err != nil {
			return nil, err
		}
	}
	turn, err := domain.NewSemanticTurn(a.TurnID(), messages)
	if err != nil {
		return nil, err
	}
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return nil, err
	}
	view, err := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{turn}}, domain.ContextSummary{}, footprint.Revision())
	if err != nil {
		return nil, err
	}
	preview, err := provider.NewInputPreview(view, footprint, func(provider.TurnInput) error { return nil })
	if err != nil {
		return nil, err
	}
	return provider.NewPreparedUserInputs(a, envelope, func() {}, func() {}, preview)
}

// submitInteractiveFixture 为已有单轮工具验收收集长期控制流中的 turn 事件。
func submitInteractiveFixture(t *testing.T, host *chatRuntime.ChatSession, ctx context.Context, text string) (<-chan protocol.Event, error) {
	t.Helper()
	result, err := host.Append(ctx, text)
	if err != nil {
		return nil, err
	}
	if result.Status() != protocol.CommandStatusAccepted {
		return nil, fmt.Errorf("input was rejected")
	}
	events := make(chan protocol.Event, 256)
	go func() {
		defer close(events)
		for item := range host.Outputs() {
			event, ok := item.TurnEvent()
			if !ok {
				continue
			}
			select {
			case events <- event:
			case <-host.Done():
				return
			}
			if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed {
				return
			}
		}
	}()
	return events, nil
}

func appendFixtureInitialInput(t *testing.T, writer interface {
	AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error)
}, turnID domain.TurnID) {
	t.Helper()
	association, err := domain.NewInputAssociation(turnID, domain.InputInitial, 0, []domain.InputID{domain.InputID(turnID)})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := session.NewTurnInputsCommittedDraft(association)
	if err != nil {
		t.Fatal(err)
	}
	native, err := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 3, Payload: []byte(`{"kind":"user_inputs"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.AppendBatch(context.Background(), []session.RecordDraft{draft, native}); err != nil {
		t.Fatal(err)
	}
}

func (conversation *orderedConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return domain.ActiveContextView{}, err
	}
	return domain.NewActiveContextView(conversation.ProjectHistory(), domain.ContextSummary{}, footprint.Revision())
}

func (conversation *fakeAppConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return domain.ActiveContextView{}, err
	}
	return domain.NewActiveContextView(conversation.ProjectHistory(), domain.ContextSummary{}, footprint.Revision())
}
