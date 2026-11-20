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
	recovery          *recoveryAttempt
	repository        *session.Repository
	expectedResumeCWD string
	load              func(context.Context, *session.JournalLease) (session.LoadResult, error)
	plan              func(session.LoadResult) (session.ReplayPlan, error)
	start             func(*session.JournalLease, session.Identity, uint64) (journalStartResult, error)
}

var errResumeDirectory = errors.New("session working directory is incompatible")

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
	title         domain.SessionTitle
	compactions   []compactionTranscript
	nativeCommits []session.NativeCommitRecord
	tools         []session.ToolHistoryEntry
	identity      session.Identity
	conversation  provider.Conversation
	writer        managedJournal
	history       domain.SemanticHistoryView
	repair        session.RepairReport
	toolRecovery  *session.ToolRecoveryPlan
	stopRecovery  *session.StopRecoveryPlan
	stops         []session.ReplayedStop
}

func newSessionService(repository *session.Repository) *sessionService {
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
	}
}

func (service *sessionService) create(
	ctx context.Context,
	binding sessionProvider,
	model string,
	creationCWD string,
) (assembledSession, error) {
	identity, err := session.GenerateRootIdentity()
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "create session identity failed", err)
	}
	conversation, err := binding.factory.NewConversation(identity.ThreadID)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeProviderUnavailable, "create conversation failed", err)
	}
	writer, _, err := session.CreateRootJournal(ctx, service.repository, identity, session.RootJournalConfig{
		Provider: binding.factory.Family(), ProviderWire: binding.wire, Model: model,
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
	binding sessionProvider,
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
			closeErr := lease.Close()
			service.recovery.cleanup(closeErr)
			resultErr = errors.Join(resultErr, closeErr)
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
	if service.expectedResumeCWD != "" && metadata.CreationCWD != service.expectedResumeCWD {
		return assembledSession{}, fault.Wrap(fault.CodeSessionIncompatible, "session working directory is incompatible", errResumeDirectory)
	}
	if metadata.Provider != binding.factory.Family() || metadata.ProviderWire != binding.wire || metadata.Model != model {
		return assembledSession{}, fault.New(fault.CodeSessionIncompatible, "session provider configuration is incompatible")
	}
	if err := ctx.Err(); err != nil {
		return assembledSession{}, err
	}
	if err := validateSampleUsageEvidence(binding.sampleUsage, plan); err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "sample usage is invalid", err)
	}
	commits, err := orderedRestoreRecords(plan)
	if err != nil {
		return assembledSession{}, err
	}

	conversation, err := binding.factory.RestoreConversation(plan.Identity.ThreadID, commits)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "restore provider native history failed", err)
	}
	if err := validateCompactionUsageEvidence(binding.compactionUsage, plan); err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "compaction usage is invalid", err)
	}
	compactions, err := restoreCompactionTranscript(binding.checkpoints, plan)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "compaction transcript is invalid", err)
	}
	if err := ctx.Err(); err != nil {
		return assembledSession{}, err
	}
	if loaded.NeedsRepair() {
		service.recovery.mutating()
		report, repairErr := session.NewLoader().Repair(ctx, lease, loaded)
		service.recovery.journalMutation(report.Repaired, repairErr)
		if repairErr != nil {
			return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "repair session journal failed", repairErr)
		}
		loaded.Repair = report
	}
	if err := ctx.Err(); err != nil {
		return assembledSession{}, err
	}
	started, err := service.start(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "start session journal writer failed", err)
	}
	writer := started.writer
	if writer == nil {
		return assembledSession{}, fault.New(fault.CodeSessionWrite, "start session journal writer failed")
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, service.recovery.closeJournal(writer))
		}
	}()
	if started.transferred {
		lease = nil
	}
	if plan.InterruptedCompaction != nil || plan.InterruptedTail != nil {
		service.recovery.mutating()
	}
	if err := reconcileInterruptedCompaction(ctx, writer, plan.InterruptedCompaction); err != nil {
		service.recovery.journalMutation(false, err)
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "close interrupted compaction failed", err)
	}
	service.recovery.journalMutation(plan.InterruptedCompaction != nil, nil)
	if plan.InterruptedTail != nil {
		failureDraft, draftErr := session.NewTurnFailedDraft(plan.InterruptedTail.TurnID, session.TurnFailedPayload{
			Code: "session_interrupted", Message: "session turn was interrupted",
		})
		if draftErr != nil {
			return assembledSession{}, fault.Wrap(
				fault.CodeSessionWrite, "build interrupted session turn failed",
				draftErr,
			)
		}
		_, appendErr := writer.AppendBatch(ctx, []session.RecordDraft{failureDraft})
		service.recovery.journalMutation(appendErr == nil, appendErr)
		if appendErr != nil {
			return assembledSession{}, fault.Wrap(
				fault.CodeSessionWrite, "close interrupted session turn failed",
				appendErr,
			)
		}
	}
	if !started.transferred {
		return assembledSession{}, fault.New(fault.CodeSessionWrite, "session journal ownership was not transferred")
	}
	if err := ctx.Err(); err != nil {
		return assembledSession{}, err
	}
	var stopRecovery *session.StopRecoveryPlan
	if plan.StopRecovery != nil {
		cloned := plan.StopRecovery.Clone()
		stopRecovery = &cloned
	}
	var toolRecovery *session.ToolRecoveryPlan
	if plan.ToolRecovery != nil {
		cloned := plan.ToolRecovery.Clone()
		toolRecovery = &cloned
	}
	return assembledSession{
		compactions: compactions, identity: plan.Identity, conversation: conversation, writer: writer,
		title: plan.Title,
		tools: plan.Tools,
		stops: plan.Stops, stopRecovery: stopRecovery,
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
			record.Commit.Payload,
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
		case domain.NativeCommitHookFeedback:
			if record.Feedback == nil {
				return nil, fault.New(fault.CodeSessionCorruption, "missing stop feedback evidence")
			}
			restored, buildErr = provider.NewStopFeedbackRestoreRecord(*record.Feedback, commit)
		case domain.NativeCommitToolOutputs:
			restored, buildErr = provider.NewToolOutputRestoreRecord(record.TurnID, commit, record.ToolOutputs)
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
