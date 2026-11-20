package openai

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
		History: &conversation.history.NativeHistory, Active: &conversation.active, Pending: &conversation.pending,
		UserEntry: userInputEntry, Feedback: stopFeedbackEntry, Encode: encodeNativeCommit,
		Validate: validateNativeHistory, Footprint: historyFootprint, Project: projectHistory,
		CheckRequest: func(entries []nativeHistoryEntry, input provider.TurnInput) error {
			_, err := conversation.compileSamplingWindow(entries, input)
			return err
		},
	}
}

type samplingRequestPlan struct {
	request responsesRequest
	purpose domain.RequestPurpose
}

func (plan samplingRequestPlan) body() (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(plan.request, maxResponsesRequestBytes)
}

func (conversation *Conversation) compileSamplingWindow(entries []nativeHistoryEntry, input provider.TurnInput) (codec.CanonicalJSON, error) {
	plan, err := conversation.planWindowSamplingRequest(input, func() []nativeHistoryEntry { return entries })
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	return plan.body()
}

func (conversation *Conversation) planSamplingRequest(history []nativeHistoryEntry, input provider.TurnInput) (samplingRequestPlan, error) {
	return conversation.planWindowSamplingRequest(input, func() []nativeHistoryEntry { return conversation.windowRequestEntries(history) })
}

func (conversation *Conversation) planWindowSamplingRequest(input provider.TurnInput, activeWindow func() []nativeHistoryEntry) (samplingRequestPlan, error) {
	sources, err := provider.ReadSamplingSources(input, conversation.Family(), activeWindow)
	if err != nil {
		return samplingRequestPlan{}, err
	}
	project := sources.Instructions != nil && sources.Instructions.HasDocuments()
	request := buildResponsesRequest(conversation.provider.config.Model, sources.History, sources.Instructions, sources.Tools)
	request.applyInference(sources.Inference)
	if sources.Skills.HasEntries() {
		index := 0
		if project {
			index = 1
		}
		items := append([]NativeItem(nil), request.Input[:index]...)
		items = append(items, NewUserItem(sources.Skills.RenderedText()))
		request.Input = append(items, request.Input[index:]...)
	}
	if conversation.provider.config.CachePolicy.Key() == domain.OpenAICacheThread {
		request.PromptCacheKey, err = domain.ThreadCacheKey(conversation.threadID)
		if err != nil {
			return samplingRequestPlan{}, err
		}
	}
	return samplingRequestPlan{request: request, purpose: domain.RequestSample}, nil
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
	if err := provider.ValidateUserInputs(a, inputs); err != nil {
		return nativeHistoryEntry{}, err
	}
	metadata, err := provider.NewInputMetadata(a)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	entry := nativeHistoryEntry{Kind: nativeHistoryUserInputs, TurnID: a.TurnID(), InputMetadata: &metadata, UserInputs: make([]NativeItem, len(inputs))}
	for i, input := range inputs {
		entry.UserInputs[i] = NewUserItem(input.Text())
		if a, ok := input.SkillAttachment(); ok {
			entry.UserInputs[i].Content = append(entry.UserInputs[i].Content, ContentPart{Type: "input_text", Text: a.Content})
			metadata.Skills = append(metadata.Skills, provider.InputSkillMetadata{InputID: input.ID(), Attachment: a})
		}
		files := input.FileAttachments()
		if files.Len() > 0 {
			metadata.Files = append(metadata.Files, provider.InputFileMetadata{InputID: input.ID(), Attachments: files})
		}
		for _, attachment := range files.Items() {
			text, err := attachment.RenderedText()
			if err != nil {
				return nativeHistoryEntry{}, err
			}
			entry.UserInputs[i].Content = append(entry.UserInputs[i].Content, ContentPart{Type: "input_text", Text: text})
		}
	}
	return entry, nil
}
