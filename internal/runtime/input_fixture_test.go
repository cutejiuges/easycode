package runtime

import (
	"context"
	"encoding/json"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func (runtime *Runtime) runFixtureTurn(ctx context.Context, inputs []domain.UserInput, emit Emitter) error {
	return runtime.runTurn(ctx, inputs, runtime.profile, nil, emit)
}

func runtimeInputs(text string) []domain.UserInput {
	input, err := domain.NewUserInput("01900000-0000-7000-8000-000000000001", text)
	if err != nil {
		panic(err)
	}
	return []domain.UserInput{input}
}

func encodeFixtureInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	if err := provider.ValidateUserInputs(a, inputs); err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	type fixtureInput struct {
		ID   domain.InputID `json:"id"`
		Text string         `json:"text"`
	}
	values := make([]fixtureInput, len(inputs))
	for i, input := range inputs {
		values[i] = fixtureInput{input.ID(), input.Text()}
	}
	bytes, err := json.Marshal(values)
	if err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	return provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", bytes)
}

func prepareFixtureInputs(a domain.InputAssociation, inputs []domain.UserInput, history domain.SemanticHistoryView, footprint domain.NativeHistoryFootprint, commit func(domain.SemanticHistoryView)) (*provider.PreparedUserInputs, error) {
	envelope, err := encodeFixtureInputs(a, inputs)
	if err != nil {
		return nil, err
	}
	candidate := history.Clone()
	messages := make([]domain.SemanticMessage, len(inputs))
	for i, input := range inputs {
		messages[i], err = domain.NewSemanticUserMessage(input.ID(), input.Text())
		if err != nil {
			return nil, err
		}
		messages[i].Files = input.FileAttachments()
	}
	if a.Placement() == domain.InputInitial {
		turn, err := domain.NewSemanticTurn(a.TurnID(), messages)
		if err != nil {
			return nil, err
		}
		candidate.Turns = append(candidate.Turns, turn)
	} else {
		last := len(candidate.Turns) - 1
		candidate.Turns[last].Messages = append(candidate.Turns[last].Messages, messages...)
	}
	view, err := domain.NewActiveContextView(candidate, domain.ContextSummary{}, footprint.Revision())
	if err != nil {
		return nil, err
	}
	preview, err := provider.NewInputPreview(view, footprint, func(provider.TurnInput) error { return nil })
	if err != nil {
		return nil, err
	}
	return provider.NewPreparedUserInputs(a, envelope, func() { commit(candidate) }, func() {}, preview)
}

func (conversation *fakeConversation) EncodeUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	return encodeFixtureInputs(a, inputs)
}
func (conversation *fakeConversation) PrepareUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedUserInputs, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return nil, err
	}
	return prepareFixtureInputs(a, inputs, conversation.ProjectHistory(), footprint, func(view domain.SemanticHistoryView) { conversation.history = view })
}
func (conversation *controlledConversation) EncodeUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	return encodeFixtureInputs(a, inputs)
}
func (conversation *controlledConversation) PrepareUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedUserInputs, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return nil, err
	}
	return prepareFixtureInputs(a, inputs, conversation.ProjectHistory(), footprint, func(view domain.SemanticHistoryView) { conversation.history = view })
}

func (conversation *fakeConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	view, _, err := conversation.ProjectContextWindow()
	return view, err
}

func (conversation *fakeConversation) ProjectContextWindow() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, err
	}
	view, err := domain.NewActiveContextView(conversation.ProjectHistory(), domain.ContextSummary{}, footprint.Revision())
	return view, footprint, err
}

func (conversation *controlledConversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	view, _, err := conversation.ProjectContextWindow()
	return view, err
}

func (conversation *controlledConversation) ProjectContextWindow() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, err
	}
	view, err := domain.NewActiveContextView(conversation.ProjectHistory(), domain.ContextSummary{}, footprint.Revision())
	return view, footprint, err
}
