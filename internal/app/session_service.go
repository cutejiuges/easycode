package app

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type sessionService struct {
	repository *session.Repository
	load       func(context.Context, *session.JournalLease) (session.LoadResult, error)
	plan       func(session.LoadResult) (session.ReplayPlan, error)
	start      func(*session.JournalLease, session.Identity, uint64) (journalStartResult, error)
}

type journalStartResult struct {
	writer      managedJournal
	transferred bool
}

type managedJournal interface {
	chatRuntimeJournal
	Close(context.Context) error
}

type chatRuntimeJournal interface {
	AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error)
	Poisoned() bool
}

type assembledSession struct {
	compactions   []compactionTranscript
	nativeCommits []session.NativeCommitRecord
	tools         []session.ToolHistoryEntry
	identity      session.Identity
	conversation  provider.Conversation
	writer        managedJournal
	history       domain.SemanticHistoryView
	repair        session.RepairReport
	toolRecovery  *session.ToolRecoveryPlan
}

func openSessionService(dataRoot string) (*sessionService, error) {
	repository, err := session.OpenOrCreateRepository(dataRoot)
	if err != nil {
		return nil, err
	}
	return &sessionService{
		repository: repository,
		load:       session.NewLoader().Load,
		plan:       session.NewReplayPlanner().Plan,
		start: func(lease *session.JournalLease, identity session.Identity, nextSequence uint64) (journalStartResult, error) {
			writer, startErr := session.StartJournalWriter(lease, identity, nextSequence)
			if startErr != nil {
				return journalStartResult{}, startErr
			}
			return journalStartResult{writer: writer, transferred: true}, nil
		},
	}, nil
}

func (service *sessionService) create(
	ctx context.Context,
	factory provider.Factory,
	wire string,
	model string,
	creationCWD string,
) (assembledSession, error) {
	identity, err := session.GenerateRootIdentity()
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "create session identity failed", err)
	}
	conversation := factory.NewConversation()
	writer, _, err := session.CreateRootJournal(ctx, service.repository, identity, session.RootJournalConfig{
		Provider: factory.Family(), ProviderWire: wire, Model: model,
		CreationCWD: creationCWD, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "create session journal failed", err)
	}
	return assembledSession{
		identity: identity, conversation: conversation, writer: writer,
		history: conversation.ProjectHistory(),
	}, nil
}

func (service *sessionService) resume(
	ctx context.Context,
	factory provider.Factory,
	wire string,
	model string,
	threadID domain.ThreadID,
) (assembled assembledSession, resultErr error) {
	lease, err := service.repository.Open(ctx, threadID)
	if err != nil {
		if session.IsJournalBusy(err) {
			return assembledSession{}, fault.New(fault.CodeSessionBusy, "session thread is already active")
		}
		if errors.Is(err, fs.ErrNotExist) {
			return assembledSession{}, fault.New(fault.CodeSessionNotFound, "session thread was not found")
		}
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "open session journal failed", err)
	}
	defer func() {
		if lease != nil {
			resultErr = errors.Join(resultErr, lease.Close())
		}
	}()
	loaded, err := service.load(ctx, lease)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "load session journal failed", err)
	}
	plan, err := service.plan(loaded)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "validate session replay failed", err)
	}
	metadata := plan.SessionMetadata
	if metadata.RootThreadID != threadID || plan.Identity.ThreadID != threadID || !plan.ThreadMetadata.Root {
		return assembledSession{}, fault.New(fault.CodeSessionIncompatible, "resume requires a root session thread")
	}
	if metadata.Provider != factory.Family() || metadata.ProviderWire != wire || metadata.Model != model {
		return assembledSession{}, fault.New(fault.CodeSessionIncompatible, "session provider configuration is incompatible")
	}
	commits, err := orderedRestoreRecords(plan)
	if err != nil {
		return assembledSession{}, err
	}

	conversation, err := factory.RestoreConversation(commits)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "restore provider native history failed", err)
	}
	if err := validateCompactionUsage(factory, plan); err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "compaction usage is invalid", err)
	}
	compactions, err := restoreCompactionTranscript(factory, plan)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "compaction transcript is invalid", err)
	}
	if loaded.NeedsRepair() {
		report, repairErr := session.NewLoader().Repair(ctx, lease, loaded)
		if repairErr != nil {
			return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "repair session journal failed", repairErr)
		}
		loaded.Repair = report
	}
	started, err := service.start(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "start session journal writer failed", err)
	}
	writer := started.writer
	if writer == nil {
		return assembledSession{}, fault.New(fault.CodeSessionWrite, "start session journal writer failed")
	}
	if started.transferred {
		lease = nil
	}
	if err := reconcileInterruptedCompaction(ctx, writer, plan.InterruptedCompaction); err != nil {
		closeErr := writer.Close(context.Background())
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "close interrupted compaction failed", errors.Join(err, closeErr))
	}
	if plan.InterruptedTail != nil {
		failureDraft, draftErr := session.NewTurnFailedDraft(plan.InterruptedTail.TurnID, session.TurnFailedPayload{
			Code: "session_interrupted", Message: "session turn was interrupted",
		})
		if draftErr != nil {
			closeErr := writer.Close(context.Background())
			return assembledSession{}, fault.Wrap(
				fault.CodeSessionWrite, "build interrupted session turn failed",
				errors.Join(draftErr, closeErr),
			)
		}
		_, appendErr := writer.AppendBatch(ctx, []session.RecordDraft{failureDraft})
		if appendErr != nil {
			closeErr := writer.Close(context.Background())
			return assembledSession{}, fault.Wrap(
				fault.CodeSessionWrite, "close interrupted session turn failed",
				errors.Join(appendErr, closeErr),
			)
		}
	}
	if !started.transferred {
		closeErr := writer.Close(context.Background())
		return assembledSession{}, fault.Wrap(
			fault.CodeSessionWrite, "session journal ownership was not transferred", closeErr,
		)
	}
	var toolRecovery *session.ToolRecoveryPlan
	if plan.ToolRecovery != nil {
		cloned := plan.ToolRecovery.Clone()
		toolRecovery = &cloned
	}
	return assembledSession{
		compactions: compactions, identity: plan.Identity, conversation: conversation, writer: writer,
		tools:         plan.Tools,
		nativeCommits: plan.NativeCommits,
		history:       conversation.ProjectHistory(), repair: loaded.Repair, toolRecovery: toolRecovery,
	}, nil
}

func (service *sessionService) close() error {
	if service == nil || service.repository == nil {
		return nil
	}
	return service.repository.Close()
}

func nativeRestoreRecords(records []session.NativeCommitRecord) ([]provider.RestoreRecord, error) {
	commits := make([]provider.RestoreRecord, 0, len(records))
	for _, record := range records {
		commit, buildErr := provider.NewNativeCommitEnvelope(
			record.Commit.Provider, record.Commit.Wire,
			record.Commit.PayloadVersion, record.Commit.Payload,
		)
		if buildErr != nil {
			return nil, fault.Wrap(fault.CodeSessionCorruption, "session native commit envelope is invalid", buildErr)
		}
		var restored provider.RestoreRecord
		switch record.Role {
		case domain.NativeCommitUserInputs:
			restored, buildErr = provider.NewInputRestoreRecord(record.Inputs, commit)
		case domain.NativeCommitSample:
			restored, buildErr = provider.NewSampleRestoreRecord(record.TurnID, commit)
		case domain.NativeCommitToolOutputs:
			restored, buildErr = provider.NewToolOutputRestoreRecord(record.TurnID, commit)
		default:
			return nil, fault.New(fault.CodeSessionCorruption, "unknown native commit role")
		}
		if buildErr != nil {
			return nil, buildErr
		}
		commits = append(commits, restored)
	}
	return commits, nil
}
