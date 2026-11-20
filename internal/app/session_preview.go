package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tui"
)

// previewDependencies 让句柄关闭、加载和取消失败可以确定性注入。
type previewDependencies struct {
	open            func(string) (*session.Repository, error)
	load            func(context.Context, *session.JournalLease) (session.LoadResult, error)
	closeLease      func(*session.JournalLease) error
	closeRepository func(*session.Repository) error
}

func defaultPreviewDependencies() previewDependencies {
	return previewDependencies{
		open: session.OpenOrCreateRepository, load: session.NewLoader().Load,
		closeLease:      func(lease *session.JournalLease) error { return lease.Close() },
		closeRepository: func(repo *session.Repository) error { return repo.Close() },
	}
}

func previewSession(ctx context.Context, root string, thread domain.ThreadID, deps previewDependencies) (result tui.SessionPreview, cleanupFailed bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return result, false, err
	}
	if !thread.Valid() {
		return result, false, fmt.Errorf("preview thread identity is invalid")
	}
	repo, err := deps.open(root)
	if err != nil {
		return result, false, err
	}
	defer func() {
		err := deps.closeRepository(repo)
		cleanupFailed = cleanupFailed || err != nil
		resultErr = errors.Join(resultErr, err)
		if resultErr == nil {
			resultErr = ctx.Err()
		}
		if resultErr != nil {
			result = tui.SessionPreview{}
		}
	}()
	lease, err := repo.Open(ctx, thread)
	if err != nil {
		return result, false, err
	}
	defer func() {
		err := deps.closeLease(lease)
		cleanupFailed = cleanupFailed || err != nil
		resultErr = errors.Join(resultErr, err)
	}()
	loaded, err := deps.load(ctx, lease)
	if err != nil {
		return result, false, err
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
	if err != nil {
		return result, false, err
	}
	meta := plan.SessionMetadata
	if plan.Identity.ThreadID != thread || meta.RootThreadID != thread || !plan.ThreadMetadata.Root {
		return result, false, fmt.Errorf("preview requires a root thread")
	}
	binding, err := bindProvider(meta.Provider)
	if err != nil || binding.wire != meta.ProviderWire {
		return result, false, fmt.Errorf("stored history source is unsupported")
	}
	if err := validateSampleUsageEvidence(binding.sampleUsage, plan); err != nil {
		return result, false, err
	}
	records, err := orderedRestoreRecords(plan)
	if err != nil {
		return result, false, err
	}
	if err := validateCompactionUsageEvidence(binding.history, plan); err != nil {
		return result, false, err
	}
	view, err := binding.history.ProjectStoredHistory(ctx, provider.HistorySource{ThreadID: thread, Family: meta.Provider, Wire: meta.ProviderWire, Model: meta.Model}, records)
	if err != nil {
		return result, false, err
	}
	result = boundSessionPreview(view)
	result.Title = plan.Title
	result.Incomplete = loaded.NeedsRepair() || plan.InterruptedTail != nil || plan.ToolRecovery != nil || plan.StopRecovery != nil || plan.InterruptedCompaction != nil
	return result, false, ctx.Err()
}

func boundSessionPreview(view domain.SemanticHistoryView) tui.SessionPreview {
	result := tui.SessionPreview{}
	remaining := 32768
	for _, turn := range view.Turns {
		for _, message := range turn.Messages {
			if message.Text == "" {
				continue
			}
			if len(result.Messages) == 128 || remaining == 0 {
				result.Truncated = true
				return result
			}
			entry := tui.PreviewMessage{TurnID: turn.TurnID, Message: message}
			entry.Files, _ = domain.NewFileSummary(message.Files)
			entry.Message.Files = domain.FileAttachments{}
			if len(message.Text) > remaining {
				end := remaining
				for end > 0 && !utf8.RuneStart(message.Text[end]) {
					end--
				}
				if end == 0 {
					result.Truncated = true
					return result
				}
				entry.Message.Text = strings.Clone(message.Text[:end])
				entry.Truncated, result.Truncated = true, true
			}
			result.Messages = append(result.Messages, entry)
			remaining -= len(entry.Message.Text)
			if result.Truncated {
				return result
			}
		}
	}
	return result
}
