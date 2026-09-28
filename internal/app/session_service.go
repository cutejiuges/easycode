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
	reopen     func(context.Context, *session.Repository, session.LoadResult) (managedJournal, error)
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
	identity     session.Identity
	conversation provider.Conversation
	writer       managedJournal
	history      domain.SemanticHistoryView
	repair       session.RepairReport
}

func newSessionService(dataRoot string) (*sessionService, error) {
	repository, err := session.NewRepository(dataRoot)
	if err != nil {
		return nil, err
	}
	return &sessionService{
		repository: repository,
		reopen: func(ctx context.Context, repository *session.Repository, loaded session.LoadResult) (managedJournal, error) {
			return session.ReopenJournalWriter(ctx, repository, loaded)
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
	identity, err := session.NewRootIdentity()
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
) (assembledSession, error) {
	loader, err := session.NewLoader(service.repository)
	if err != nil {
		return assembledSession{}, err
	}
	loaded, err := loader.Load(ctx, threadID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return assembledSession{}, fault.New(fault.CodeSessionNotFound, "session thread was not found")
		}
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "load session journal failed", err)
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
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
	commits := make([]provider.NativeCommitEnvelope, 0, len(plan.NativeCommits))
	for _, record := range plan.NativeCommits {
		commit, buildErr := provider.NewNativeCommitEnvelope(
			record.Commit.Provider, record.Commit.Wire,
			record.Commit.PayloadVersion, record.Commit.Payload,
		)
		if buildErr != nil {
			return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "session native commit envelope is invalid", buildErr)
		}
		commits = append(commits, commit)
	}
	conversation, err := factory.RestoreConversation(commits)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionCorruption, "restore provider native history failed", err)
	}
	writer, err := service.reopen(ctx, service.repository, loaded)
	if err != nil {
		return assembledSession{}, fault.Wrap(fault.CodeSessionWrite, "reopen session journal failed", err)
	}
	if plan.InterruptedTail != nil {
		_, appendErr := writer.AppendBatch(ctx, []session.RecordDraft{{
			EventKind: session.EventTurnFailed,
			TurnID:    plan.InterruptedTail.TurnID,
			Payload: session.TurnFailedPayload{
				Code: "session_interrupted", Message: "session turn was interrupted",
			},
		}})
		if appendErr != nil {
			closeErr := writer.Close(context.Background())
			return assembledSession{}, fault.Wrap(
				fault.CodeSessionWrite, "close interrupted session turn failed",
				errors.Join(appendErr, closeErr),
			)
		}
	}
	return assembledSession{
		identity: plan.Identity, conversation: conversation, writer: writer,
		history: conversation.ProjectHistory(), repair: plan.Repair,
	}, nil
}

func (service *sessionService) close() error {
	if service == nil || service.repository == nil {
		return nil
	}
	return service.repository.Close()
}
