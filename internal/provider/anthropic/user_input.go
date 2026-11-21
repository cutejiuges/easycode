package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"fmt"
)

var _ provider.UserInputPreparer = (*Conversation)(nil)

// PrepareUserInputs 纯内存冻结有序输入及同一版本历史的预检快照。
func (conversation *Conversation) PrepareUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedUserInputs, error) {
	if err := provider.ValidateUserInputs(a, inputs); err != nil {
		return nil, err
	}
	if conversation == nil {
		return nil, fmt.Errorf("conversation is active")
	}
	return conversation.inputPreparation().PrepareInputs(a, inputs)
}

func (conversation *Conversation) inputPreparation() provider.NativeInputPreparation[nativeHistoryEntry] {
	return provider.NativeInputPreparation[nativeHistoryEntry]{
		History: &conversation.history, Operations: &conversation.operations,
		UserEntry: userInputEntry, Feedback: stopFeedbackEntry, Encode: encodeNativeCommit,
		Validate: validateNativeHistory, Footprint: historyFootprint, Project: projectHistory,
		CheckRequest: func(entries []nativeHistoryEntry, input provider.TurnInput) error {
			_, err := conversation.compileSamplingWindow(entries, input)
			return err
		},
	}
}

func (plan samplingRequestPlan) body(mode domain.AnthropicCacheMode) (codec.CanonicalJSON, error) {
	request, err := plan.compile(mode)
	return request.body, err
}

type samplingRequestPlan struct {
	purpose domain.RequestPurpose
	request messagesRequest
	project bool
	skills  bool
}

func (conversation *Conversation) compileSamplingWindow(entries []nativeHistoryEntry, input provider.TurnInput) (codec.CanonicalJSON, error) {
	plan, err := conversation.planWindowSamplingRequest(input, func() []nativeHistoryEntry { return entries })
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	return plan.body(conversation.provider.config.CachePolicy.Anthropic())
}

func (conversation *Conversation) planSamplingRequest(window nativeWindowSnapshot, input provider.TurnInput) (samplingRequestPlan, error) {
	return conversation.planWindowSamplingRequest(input, func() []nativeHistoryEntry { return window.RequestEntries() })
}

func (conversation *Conversation) planWindowSamplingRequest(input provider.TurnInput, activeWindow func() []nativeHistoryEntry) (samplingRequestPlan, error) {
	sources, err := provider.ReadSamplingSources(input, conversation.Family(), activeWindow)
	if err != nil {
		return samplingRequestPlan{}, err
	}
	project := sources.Instructions != nil && sources.Instructions.HasDocuments()
	request := buildMessagesRequest(conversation.provider.config.Model, int(sources.Inference.MaxOutputTokens()), sources.History, sources.Instructions, sources.Tools)
	request.System = sources.WorkMode.Instructions()
	request.applyInference(sources.Inference)
	if sources.Skills.HasEntries() {
		index := 0
		if project {
			index = 1
		}
		items := append([]nativeMessage(nil), request.Messages[:index]...)
		items = append(items, newUserMessage(sources.Skills.RenderedText()))
		request.Messages = append(items, request.Messages[index:]...)
		if request.lastSampleMessage >= 0 {
			request.lastSampleMessage++
		}
	}
	return samplingRequestPlan{
		purpose: domain.RequestSample, request: request,
		project: project, skills: sources.Skills.HasEntries(),
	}, nil
}

// EncodeUserInputs 不读取会话状态，仅验证用户消息的可表示性。
func (conversation *Conversation) EncodeUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	entry, err := userInputEntry(a, inputs)
	if err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	return encodeNativeCommit(entry)
}

func userInputEntry(a domain.InputAssociation, inputs []domain.UserInput) (nativeHistoryEntry, error) {
	metadata, messages, err := provider.EncodeUserInputMessages(a, inputs, func(blocks []string) nativeMessage {
		content := make([]NativeItem, len(blocks))
		for i, text := range blocks {
			content[i] = NativeItem{Type: blockTypeText, Text: text}
		}
		return nativeMessage{Role: roleUser, Content: content}
	})
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, TurnID: a.TurnID(), InputMetadata: &metadata, UserInputs: messages}, nil
}
