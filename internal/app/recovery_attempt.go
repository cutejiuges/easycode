package app

import (
	"context"
	"errors"

	"easycode/internal/session"
	"easycode/internal/session/artifact"
)

// recoveryAttempt 分别记录恢复事实与最终资源清理，由恢复宿主独占。
type recoveryAttempt struct {
	startupMutation bool
	committed       bool
	uncertain       bool
	artifacts       artifact.CleanupReport
	cleanupErr      error
	recoveryJournal *recoveryJournal
}

func (a *recoveryAttempt) mutating() {
	if a != nil {
		a.startupMutation = true
	}
}

func (a *recoveryAttempt) journalMutation(committed bool, err error) {
	if a == nil {
		return
	}
	a.committed = a.committed || committed
	a.uncertain = a.uncertain || session.IsJournalMutationUncertain(err)
}

func (a *recoveryAttempt) artifactMaintenance(report artifact.CleanupReport) {
	if a != nil {
		a.artifacts = report
	}
}

func (a *recoveryAttempt) cleanup(err error) {
	if a != nil && err != nil {
		a.cleanupErr = errors.Join(a.cleanupErr, err)
	}
}

// fatal 保留建立初始聊天时的失败判据。
func (a *recoveryAttempt) fatal() bool {
	return a != nil && (a.startupMutation || a.committed || a.uncertain || a.cleanupErr != nil)
}

func (a *recoveryAttempt) closeJournal(writer managedJournal) error {
	if a == nil {
		return finishManagedJournal(context.Background(), writer).err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	outcome := finishManagedJournal(ctx, writer)
	err := outcome.cleanupError()
	if ctx.Err() == context.DeadlineExceeded && !errors.Is(err, context.DeadlineExceeded) {
		err = errors.Join(err, context.DeadlineExceeded)
	}
	a.cleanup(err)
	return err
}

func (a *recoveryAttempt) journalForRecovery(writer managedJournal) chatRuntimeJournal {
	if a == nil {
		return writer
	}
	a.recoveryJournal = &recoveryJournal{writer: writer, attempt: a}
	return a.recoveryJournal
}

func (a *recoveryAttempt) finishJournalRecovery() {
	if a != nil && a.recoveryJournal != nil {
		a.recoveryJournal.attempt = nil
		a.recoveryJournal = nil
	}
}

// recoveryJournal 只在本地恢复阶段观察真实批次的持久化结果。
type recoveryJournal struct {
	writer  managedJournal
	attempt *recoveryAttempt
}

func (journal *recoveryJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	records, err := journal.writer.AppendBatch(ctx, drafts)
	journal.attempt.journalMutation(err == nil, err)
	return records, err
}

func (journal *recoveryJournal) Poisoned() bool { return journal.writer.Poisoned() }

type journalCloseReporter interface {
	CloseResult(context.Context) (session.JournalCloseResult, error)
}

type managedJournalClose struct {
	result      session.JournalCloseResult
	waitErr     error
	fallbackErr error
}

func (outcome managedJournalClose) cleanupError() error {
	return errors.Join(outcome.waitErr, outcome.result.CleanupError(), outcome.fallbackErr)
}

func (outcome managedJournalClose) err() error {
	return errors.Join(outcome.waitErr, outcome.result.Err(), outcome.fallbackErr)
}

// finishManagedJournal 等待同一 writer 的最终结果，并保留首次等待取消。
func finishManagedJournal(ctx context.Context, writer managedJournal) managedJournalClose {
	if reporter, ok := writer.(journalCloseReporter); ok {
		result, waitErr := reporter.CloseResult(ctx)
		var finalWaitErr error
		if waitErr != nil && ctx.Err() != nil {
			result, finalWaitErr = reporter.CloseResult(context.Background())
		}
		return managedJournalClose{result: result, waitErr: errors.Join(waitErr, finalWaitErr)}
	}
	err := writer.Close(ctx)
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return managedJournalClose{waitErr: err, fallbackErr: writer.Close(context.Background())}
	}
	return managedJournalClose{fallbackErr: err}
}
