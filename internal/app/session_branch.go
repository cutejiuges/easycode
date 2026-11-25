package app

import (
	"context"
	"errors"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
)

// branchCreation 由交接 worker 持有，保留发布与后续装配各自的结果。
type branchCreation struct {
	source      *session.JournalWriter
	target      domain.ThreadID
	title       domain.SessionTitle
	historical  *session.HistoricalCut
	admit       func(domain.ThreadID) bool
	publication session.BranchPublication
}

func (service *sessionService) branch(
	ctx context.Context,
	binding sessionProvider,
	model string,
	creation *branchCreation,
	checkEnvironment func(context.Context) error,
) (_ assembledSession, resultErr error) {
	var stage *session.BranchStage
	var loaded session.LoadResult
	var err error
	if creation.historical == nil {
		stage, loaded, err = service.repository.PrepareBranch(ctx, creation.source, creation.target, creation.title)
	} else {
		stage, loaded, err = service.repository.PrepareHistoricalBranch(ctx, creation.source, creation.target, creation.title, *creation.historical)
	}
	if err != nil {
		if session.IsBranchCleanupFailure(err) {
			service.recovery.cleanup(err)
		}
		switch {
		case session.IsBranchSourceEmpty(err):
			return assembledSession{}, fault.Wrap(fault.CodeInvalidInput, "current session has no committed user input", err)
		case session.IsHistoricalCutStale(err):
			return assembledSession{}, fault.Wrap(fault.CodeInvalidInput, "historical branch selection expired; reopen rewind", err)
		case session.IsBranchSourceUnsafe(err):
			return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "source session committed history is unavailable", err)
		case ctx.Err() != nil:
			return assembledSession{}, ctx.Err()
		default:
			return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "prepare session branch failed", err)
		}
	}
	defer func() {
		if stage != nil {
			closeErr := stage.Close()
			service.recovery.cleanup(closeErr)
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()

	validated, _, err := service.restore(ctx, binding, model, creation.target, loaded)
	if err != nil {
		if fault.Project(err).Code == fault.CodeSessionCorruption {
			return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "source session committed history is invalid", creation.source.MarkBranchSourceUnsafe(err))
		}
		return assembledSession{}, err
	}
	if err := stage.Sync(ctx); err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "sync staged session branch failed", err)
	}
	if err := checkEnvironment(ctx); err != nil {
		return assembledSession{}, fault.New(fault.CodeInvalidConfiguration, "session branch workspace or storage is unavailable")
	}
	if !creation.admit(creation.target) {
		return assembledSession{}, fault.New(fault.CodeUserCancelled, "session branch publication was not admitted")
	}
	creation.publication, err = stage.Publish()
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "publish session branch failed", err)
	}
	if err := ctx.Err(); err != nil {
		return assembledSession{}, err
	}
	lease, err := stage.TakeLease()
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "take session branch journal lease failed", err)
	}
	closeErr := stage.Close()
	stage = nil
	if closeErr != nil {
		closeErr = errors.Join(closeErr, lease.Close())
		service.recovery.cleanup(closeErr)
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "close session branch stage failed", closeErr)
	}
	writer, err := session.StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		closeErr := lease.Close()
		service.recovery.cleanup(closeErr)
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "start session branch writer failed", errors.Join(err, closeErr))
	}
	validated.writer = writer
	return validated, nil
}
