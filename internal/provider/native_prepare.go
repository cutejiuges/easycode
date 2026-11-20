package provider

import (
	"fmt"
	"math"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// NativeInputPreparation 组合候选输入构造、验证与原生窗口预检所需的策略。
type NativeInputPreparation[E nativeClone[E]] struct {
	History      *NativeHistory[E]
	Operations   *ConversationOperations
	UserEntry    func(domain.InputAssociation, []domain.UserInput) (E, error)
	Feedback     func(domain.StopFeedback) (E, error)
	Encode       func(E) (NativeCommitEnvelope, error)
	Validate     func([]E) error
	Footprint    func([]E, uint64) (domain.NativeHistoryFootprint, error)
	Project      func([]E) domain.SemanticHistoryView
	CheckRequest func([]E, TurnInput) error
}

func prepareNativeCommit[P any](operations *ConversationOperations, activeMessage, pendingMessage string, prepare func() (P, error)) (P, error) {
	var zero P
	switch operations.begin(ConversationPending) {
	case ConversationActive:
		return zero, fmt.Errorf("%s", activeMessage)
	case ConversationPending:
		return zero, fmt.Errorf("%s", pendingMessage)
	}
	prepared := false
	defer func() {
		if !prepared {
			operations.ResolvePrepared()
		}
	}()
	value, err := prepare()
	prepared = err == nil
	return value, err
}

// PrepareInputs 准备已验证的用户输入，保持先验证候选再编码提交的顺序。
func (p NativeInputPreparation[E]) PrepareInputs(association domain.InputAssociation, inputs []domain.UserInput) (*PreparedUserInputs, error) {
	return prepareNativeCommit(p.Operations, "conversation is active", "conversation has an unresolved commit", func() (*PreparedUserInputs, error) {
		entry, err := p.UserEntry(association, inputs)
		if err != nil {
			return nil, err
		}
		window := p.History.Snapshot()
		window.Entries = append(window.Entries, entry.CloneNativeEntry())
		window.Revision++
		if err := p.Validate(window.Entries); err != nil {
			return nil, err
		}
		envelope, err := p.Encode(entry)
		if err != nil {
			return nil, err
		}
		preview, err := p.preview(window)
		if err != nil {
			return nil, err
		}
		return NewPreparedUserInputs(association, envelope, func() {
			p.History.Append(entry)
			p.Operations.ResolvePrepared()
		}, p.Operations.ResolvePrepared, preview)
	})
}

// PrepareStop 准备单独反馈或反馈加用户纠正的一次性原生交付。
func (p NativeInputPreparation[E]) PrepareStop(input domain.StopContinuationInput) (*PreparedStopContinuation, error) {
	return prepareNativeCommit(p.Operations, "conversation is active", "conversation has an unresolved commit", func() (*PreparedStopContinuation, error) {
		window := p.History.Snapshot()
		feedback, err := p.Feedback(input.Feedback())
		if err != nil {
			return nil, err
		}
		feedbackEnvelope, err := p.Encode(feedback)
		if err != nil {
			return nil, err
		}

		entries := []E{feedback}
		var userEnvelope NativeCommitEnvelope
		if association, ok := input.Association(); ok {
			user, err := p.UserEntry(association, input.Inputs())
			if err != nil {
				return nil, err
			}
			userEnvelope, err = p.Encode(user)
			if err != nil {
				return nil, err
			}
			entries = append(entries, user)
		}
		if window.Revision > math.MaxUint64-uint64(len(entries)) {
			return nil, fmt.Errorf("native history revision overflow")
		}
		window.Entries = append(window.Entries, entries...)
		window.Revision += uint64(len(entries))
		if err := p.Validate(window.Entries); err != nil {
			return nil, err
		}
		preview, err := p.preview(window)
		if err != nil {
			return nil, err
		}
		finalize := func() {
			p.History.AppendBatch(entries)
			p.Operations.ResolvePrepared()
		}
		discard := p.Operations.ResolvePrepared
		if input.Kind() == domain.StopWithSteering {
			return NewPreparedSteeredStopContinuation(input, feedbackEnvelope, userEnvelope, finalize, discard, preview)
		}
		return NewPreparedStopContinuation(input, feedbackEnvelope, finalize, discard, preview)
	})
}

func (p NativeInputPreparation[E]) preview(window NativeWindow[E]) (InputPreview, error) {
	if window.Revision > math.MaxUint64-window.Count {
		return InputPreview{}, fmt.Errorf("context revision overflow")
	}
	entries := window.RequestEntries()
	footprint, err := p.Footprint(entries, window.Revision+window.Count)
	if err != nil {
		return InputPreview{}, err
	}
	view, err := ProjectNativeWindow(window, p.Project)
	if err != nil {
		return InputPreview{}, err
	}
	return NewInputPreview(view, footprint, func(input TurnInput) error {
		return p.CheckRequest(entries, input)
	})
}

// PrepareNativeToolOutputs 在已有活动与待提交边界内准备工具输出。
func PrepareNativeToolOutputs[E nativeClone[E]](history *NativeHistory[E], operations *ConversationOperations, activeMessage, pendingMessage string, results []tool.ModelOutput, prepare func([]E, []tool.ModelOutput) (E, error), encode func(E) (NativeCommitEnvelope, error)) (*PreparedToolOutputs, error) {
	return prepareNativeCommit(operations, activeMessage, pendingMessage, func() (*PreparedToolOutputs, error) {
		entries := history.Entries()
		entry, err := prepare(entries, results)
		if err != nil {
			return nil, err
		}
		envelope, err := encode(entry)
		if err != nil {
			return nil, err
		}
		return NewPreparedToolOutputs(envelope, func() {
			history.Append(entry)
			operations.ResolvePrepared()
		}, operations.ResolvePrepared)
	})
}
