package provider

import (
	"fmt"
	"iter"

	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

// ProjectNativeHistory 保留回合顺序，交由适配器提取各自允许展示的正文。
func ProjectNativeHistory[E nativeSequenceEntry, U any](entries []E, family domain.ProviderFamily, users func(E) []U, userText func(U, bool) string, assistant func(E) string, feedback func(E) (domain.StopFeedback, error), sessionStart func(E) (domain.SessionStartContext, error)) domain.SemanticHistoryView {
	view := domain.SemanticHistoryView{Provider: family, Turns: make([]domain.SemanticTurn, 0)}
	messageCount := 0
	for _, entry := range entries {
		role, turn := entry.NativeRole(), entry.NativeTurn()
		if role == domain.NativeCommitToolOutputs {
			continue
		}
		if role == domain.NativeCommitSessionStartContext && entry.NativeSessionStartTarget().Source == domain.SessionStartCompact {
			value, err := sessionStart(entry)
			if err == nil {
				view.CheckpointContexts = append(view.CheckpointContexts, domain.SemanticCheckpointContext{AfterMessages: messageCount, Context: value})
			}
			continue
		}
		if len(view.Turns) == 0 || view.Turns[len(view.Turns)-1].TurnID != turn {
			view.Turns = append(view.Turns, domain.SemanticTurn{TurnID: turn, Messages: make([]domain.SemanticMessage, 0)})
		}
		messages := &view.Turns[len(view.Turns)-1].Messages
		before := len(*messages)
		switch role {
		case domain.NativeCommitSessionStartContext:
			value, err := sessionStart(entry)
			if err == nil {
				message, err := domain.NewSemanticSessionStartContext(value)
				if err == nil {
					*messages = append(*messages, message)
				}
			}
		case domain.NativeCommitHookFeedback:
			value, err := feedback(entry)
			if err == nil {
				message, err := domain.NewSemanticHookMessage(value)
				if err == nil {
					*messages = append(*messages, message)
				}
			}
		case domain.NativeCommitUserInputs:
			metadata := entry.NativeInputMetadata()
			for i, input := range users(entry) {
				id := metadata.InputIDs[i]
				skill, hasSkill := metadata.Skill(id)
				files := metadata.FileAttachments(id)
				*messages = append(*messages, domain.SemanticMessage{
					Role: domain.SemanticUser, InputID: id, Text: userText(input, hasSkill || files.Len() > 0),
					Skill: skill, HasSkill: hasSkill, Files: files,
				})
			}
		default:
			if text := assistant(entry); text != "" {
				*messages = append(*messages, domain.SemanticMessage{Role: domain.SemanticAssistant, Text: text})
			}
		}
		messageCount += len(*messages) - before
	}
	return view
}

// ProjectNativeTranscript 保留展示条目与原生位置之间的关联。
func ProjectNativeTranscript[E nativeSequenceEntry, U any](entries []E, start uint64, family domain.ProviderFamily, users func(E) []U, userText func(U) string, sample func([]domain.TranscriptItem, E, uint64) ([]domain.TranscriptItem, error), feedback func(E) (domain.StopFeedback, error), sessionStart func(E) (domain.SessionStartContext, error)) (domain.TranscriptView, error) {
	items := make([]domain.TranscriptItem, 0)
	for ordinal, entry := range entries {
		position := domain.TranscriptPosition{Entry: start + uint64(ordinal)}
		switch entry.NativeRole() {
		case domain.NativeCommitSessionStartContext:
			value, err := sessionStart(entry)
			if err != nil {
				return domain.TranscriptView{}, err
			}
			item, err := domain.NewTranscriptSessionStartContext(position, value)
			if err != nil {
				return domain.TranscriptView{}, err
			}
			items = append(items, item)
		case domain.NativeCommitToolOutputs:
			continue
		case domain.NativeCommitHookFeedback:
			value, err := feedback(entry)
			if err != nil {
				return domain.TranscriptView{}, err
			}
			item, err := domain.NewTranscriptHook(position, value)
			if err != nil {
				return domain.TranscriptView{}, err
			}
			items = append(items, item)
		case domain.NativeCommitUserInputs:
			inputs, metadata := users(entry), entry.NativeInputMetadata()
			if metadata == nil || len(metadata.InputIDs) != len(inputs) {
				return domain.TranscriptView{}, fmt.Errorf("invalid transcript input association")
			}
			for index, input := range inputs {
				position.Item = uint64(index)
				id := metadata.InputIDs[index]
				item, err := domain.NewTranscriptUser(position, entry.NativeTurn(), id, userText(input))
				if err != nil {
					return domain.TranscriptView{}, err
				}
				item.Skill, item.HasSkill = metadata.Skill(id)
				item.Files = metadata.FileAttachments(id)
				items = append(items, item)
			}
		case domain.NativeCommitSample:
			part, err := sample(items, entry, position.Entry)
			if err != nil {
				return domain.TranscriptView{}, err
			}
			items = part
		default:
			return domain.TranscriptView{}, fmt.Errorf("invalid transcript entry")
		}
	}
	return domain.NewTranscriptView(family, items)
}

// ProjectNativeAssistantSample 定位 Stop 检查所引用的已提交 sample。
func ProjectNativeAssistantSample[E nativeSequenceEntry](entries []E, reference domain.AssistantSampleReference, assistant func(E) string) (domain.AssistantSampleText, error) {
	var index uint32
	for _, entry := range entries {
		if entry.NativeTurn() != reference.TurnID || entry.NativeRole() != domain.NativeCommitSample {
			continue
		}
		if index == reference.SampleIndex {
			return domain.NewAssistantSampleText(reference, assistant(entry))
		}
		index++
	}
	return domain.AssistantSampleText{}, fmt.Errorf("committed assistant sample was not found")
}

// LatestNativeAssistantSample 只复制指定回合的最终原生样本，不复制已提交前缀。
func LatestNativeAssistantSample[E interface {
	nativeClone[E]
	nativeSequenceEntry
}](history *NativeHistory[E], reference domain.AssistantSampleReference) (E, bool) {
	var zero E
	if reference.Validate() != nil {
		return zero, false
	}
	history.mu.RLock()
	entries := history.entries
	history.mu.RUnlock()
	lastIndex := lastNativeConversationIndex(entries)
	if lastIndex < 0 {
		return zero, false
	}
	last := entries[lastIndex]
	if last.NativeTurn() != reference.TurnID || last.NativeRole() != domain.NativeCommitSample {
		return zero, false
	}
	var index uint32
	for i := lastIndex - 1; i >= 0; i-- {
		if entries[i].NativeRole() == domain.NativeCommitSessionStartContext {
			continue
		}
		if entries[i].NativeTurn() != reference.TurnID {
			break
		}
		if entries[i].NativeRole() == domain.NativeCommitSample {
			index++
		}
	}
	if index != reference.SampleIndex {
		return zero, false
	}
	return last.CloneNativeEntry(), true
}

// EstimateNativeHistory 按适配器的请求条目边界估算原生字节占用。
func EstimateNativeHistory[E, W any](entries []E, revision uint64, family domain.ProviderFamily, inputs func(E) iter.Seq[W]) (domain.NativeHistoryFootprint, error) {
	var bytes uint64
	for _, entry := range entries {
		for item := range inputs(entry) {
			encoded, err := codec.MarshalStable(item)
			if err != nil {
				tokens, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
				return domain.NewNativeHistoryFootprint(family, revision, tokens)
			}
			bytes = estimate.SaturatingAdd(bytes, uint64(len(encoded)))
		}
	}
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, estimate.ByteCount(bytes))
	return domain.NewNativeHistoryFootprint(family, revision, tokens)
}
