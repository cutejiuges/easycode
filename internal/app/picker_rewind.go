package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tui"
)

type rewindCandidate struct {
	cut      session.HistoricalCut
	textHash [sha256.Size]byte
	hasSkill bool
}

var errRewindPromptUnavailable = errors.New("historical prompt is unavailable; reopen rewind")

func newRewindPickerExecutor(binding providerBinding, model, cwd string) pickerRewindExecutor {
	return func(ctx context.Context, request tui.PickerRequest, source *chatResources) (tui.PickerResult, map[tui.RewindSelection]rewindCandidate) {
		result := tui.PickerResult{Request: request}
		writer, ok := source.writer.(*session.JournalWriter)
		if !ok {
			result.Error = "Source session journal is unavailable"
			return result, nil
		}
		fail := func(err error) (tui.PickerResult, map[tui.RewindSelection]rewindCandidate) {
			result.Error = pickerFailure(err)
			result.Fatal = session.IsBranchSourceUnsafe(err)
			return result, nil
		}
		if err := source.artifactLayout.Check(ctx); err != nil {
			return fail(fault.Wrap(fault.CodeInvalidConfiguration, "session workspace or storage is unavailable", err))
		}
		loaded, err := writer.ReadHistorySnapshot(ctx)
		if err != nil {
			if session.IsBranchSourceUnsafe(err) {
				return fail(fault.Wrap(fault.CodeSessionCorruption, "source session committed history is unavailable", err))
			}
			return fail(fault.Wrap(fault.CodeInvalidInput, "session rewind history is unavailable", err))
		}

		service := *source.service
		service.expectedResumeCWD = cwd
		restored, replay, err := service.restore(ctx, binding.bindSession(source.provider), model, request.Source, loaded)
		if err != nil {
			if fault.Project(err).Code == fault.CodeSessionCorruption {
				err = fault.Wrap(fault.CodeSessionCorruption, "source session committed history is invalid", writer.MarkBranchSourceUnsafe(err))
			}
			return fail(err)
		}
		if loaded.Identity != source.identity || replay.InterruptedTail != nil || replay.ToolRecovery != nil ||
			replay.StopRecovery != nil || replay.InterruptedCompaction != nil || replay.InterruptedPostCompact != nil ||
			replay.InterruptedSessionStart != nil {
			err := writer.MarkBranchSourceUnsafe(errors.New("session rewind requires a closed source journal"))
			return fail(fault.Wrap(fault.CodeSessionCorruption, "source session committed history is invalid", err))
		}

		entries, candidates, more, err := rewindEntries(ctx, replay, restored.history)
		if err != nil {
			return fail(fault.Wrap(fault.CodeInvalidInput, "session rewind prompts are unavailable", err))
		}
		result.RewindEntries, result.HasMore = entries, more
		return result, candidates
	}
}

func rewindEntries(ctx context.Context, replay session.ReplayPlan, history domain.SemanticHistoryView) ([]tui.RewindEntry, map[tui.RewindSelection]rewindCandidate, bool, error) {
	cuts, more := replay.HistoricalCuts()
	if len(cuts) == 0 {
		return nil, nil, false, nil
	}
	positions := make(map[tui.RewindSelection]int, len(cuts))
	entries := make([]tui.RewindEntry, len(cuts))
	candidates := make(map[tui.RewindSelection]rewindCandidate, len(cuts))
	for index, cut := range cuts {
		identity := tui.RewindSelection{TurnID: cut.TurnID(), InputID: cut.InputID()}
		positions[identity] = index
	}
	for _, turn := range history.Turns {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		for _, message := range turn.Messages {
			if message.Role != domain.SemanticUser {
				continue
			}
			identity := tui.RewindSelection{TurnID: turn.TurnID, InputID: message.InputID}
			index, ok := positions[identity]
			if !ok {
				continue
			}
			if _, duplicate := candidates[identity]; duplicate || domain.ValidateUserText(message.Text) != nil {
				return nil, nil, false, errRewindPromptUnavailable
			}
			cut := cuts[index]
			entries[index] = tui.RewindEntry{
				RewindSelection: identity, Number: cut.Number(), Summary: rewindPreview(message.Text), HasMultipleInputs: cut.HasMultipleInputs(),
			}
			candidates[identity] = rewindCandidate{cut: cut, textHash: sha256.Sum256([]byte(message.Text)), hasSkill: message.HasSkill}
		}
	}
	if len(candidates) != len(cuts) {
		return nil, nil, false, errRewindPromptUnavailable
	}
	return entries, candidates, more, nil
}

func rewindPreview(text string) string {
	const limit = 512
	if len(text) > limit {
		end := limit
		for !utf8.RuneStart(text[end]) {
			end--
		}
		text = text[:end]
	}
	return strings.Clone(text)
}

func (q *transcriptQueries) rewindDraft(ctx context.Context, selection tui.RewindSelection, candidate rewindCandidate) (tui.RewindDraft, error) {
	ctx, done, err := q.begin(ctx)
	if err != nil {
		return tui.RewindDraft{}, err
	}
	defer done()
	cursor := protocol.TranscriptCursor{Sequence: candidate.cut.InputSequence()}
	q.mu.Lock()
	index := sort.Search(q.nodes.len(), func(i int) bool { return !q.nodes.at(i).cursor.Before(cursor) })
	var item domain.TranscriptItem
	if index < q.nodes.len() && q.nodes.at(index).cursor == cursor {
		item = q.nodes.at(index).item
	}
	q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return tui.RewindDraft{}, err
	}
	if item.Kind != domain.TranscriptUser || item.TurnID != selection.TurnID || item.InputID != selection.InputID ||
		item.HasSkill != candidate.hasSkill || sha256.Sum256([]byte(item.Text)) != candidate.textHash {
		return tui.RewindDraft{}, errRewindPromptUnavailable
	}
	return tui.RewindDraft{Text: item.Text, HasSkill: item.HasSkill}, nil
}
