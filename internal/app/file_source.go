package app

import (
	"context"
	"errors"
	"sync"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type workspaceFileBoundary interface {
	workspaceFileReader
	DiscoverFileNames(context.Context, func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error)
}
type skillInputReader interface {
	Prepare(context.Context, string) (domain.SkillAttachment, bool, error)
}
type filePreparationOwner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// workspaceInputSource 拥有文件搜索 worker 和接纳前准备栈；不拥有 Workspace 的最终关闭。
type workspaceInputSource struct {
	workspace                 workspaceFileBoundary
	skills                    skillInputReader
	mu                        sync.Mutex
	started, closed           bool
	stop                      context.CancelFunc
	preparation               *filePreparationOwner
	latest                    domain.FileSearchQuery
	hasQuery                  bool
	cancelledIdentity         domain.FileSearchIdentity
	activeIdentity            domain.FileSearchIdentity
	cancelSearch, cancelMatch context.CancelFunc
	wake                      chan struct{}
	results                   chan domain.FileSearchSnapshot
	ready, done               chan struct{}
}

// newWorkspaceInputSource 只组合既有能力，不启动 worker 或读取文件。
func newWorkspaceInputSource(workspace workspaceFileBoundary, skills skillInputReader) (*workspaceInputSource, error) {
	if workspace == nil || skills == nil {
		return nil, domain.NewFileFailure(domain.FileInvalid)
	}
	return &workspaceInputSource{workspace: workspace, skills: skills, wake: make(chan struct{}, 1), results: make(chan domain.FileSearchSnapshot, 1), ready: make(chan struct{}), done: make(chan struct{})}, nil
}

func (s *workspaceInputSource) Start(ctx context.Context) error {
	if ctx == nil {
		return domain.NewFileFailure(domain.FileInvalid)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	workerContext, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()
		cancel()
		return domain.NewFileFailure(domain.FileUnavailable)
	}
	s.started = true
	s.stop = cancel
	s.mu.Unlock()
	go s.run(workerContext)
	select {
	case <-s.ready:
		if ctx.Err() == nil {
			return nil
		}
	case <-ctx.Done():
	}
	s.BeginClose()
	<-s.done
	return ctx.Err()
}

// BeginClose 先关接收门和取消，再由原 worker 等待全部准备栈真实退出。
func (s *workspaceInputSource) BeginClose() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.hasQuery = false
	stop, preparation := s.stop, s.preparation
	started := s.started
	s.mu.Unlock()
	if preparation != nil {
		preparation.cancel()
	}
	if stop != nil {
		stop()
	}
	if !started {
		close(s.results)
		close(s.done)
	}
}

func (s *workspaceInputSource) Close(ctx context.Context) error {
	if ctx == nil {
		return domain.NewFileFailure(domain.FileInvalid)
	}
	s.BeginClose()
	select {
	case <-s.done:
		return nil
	default:
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *workspaceInputSource) Done() <-chan struct{}                         { return s.done }
func (s *workspaceInputSource) FileResults() <-chan domain.FileSearchSnapshot { return s.results }

func (s *workspaceInputSource) beginPreparation(ctx context.Context) (context.Context, *filePreparationOwner, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	readContext, cancel := context.WithCancel(ctx)
	owner := &filePreparationOwner{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	if !s.started || s.closed {
		s.mu.Unlock()
		cancel()
		return nil, nil, domain.NewFileFailure(domain.FileUnavailable)
	}
	if s.preparation != nil {
		s.mu.Unlock()
		cancel()
		return nil, nil, domain.NewFileFailure(domain.FileBusy)
	}
	s.preparation = owner
	s.mu.Unlock()
	return readContext, owner, nil
}

func (s *workspaceInputSource) finishPreparation(owner *filePreparationOwner) {
	owner.cancel()
	s.mu.Lock()
	close(owner.done)
	if s.preparation == owner {
		s.preparation = nil
	}
	s.mu.Unlock()
}

// Prepare 在同一受控借用中组合 Skill 与文件，全部静态引用校验早于任一来源 I/O。
func (s *workspaceInputSource) Prepare(ctx context.Context, command protocol.Command) (protocol.Command, error) {
	if ctx == nil {
		return protocol.Command{}, domain.NewFileFailure(domain.FileInvalid)
	}
	if err := command.Validate(); err != nil {
		return protocol.Command{}, err
	}
	if command.Kind() != protocol.CommandSubmitInput && command.Kind() != protocol.CommandSubmitConversationalInput {
		return command, nil
	}
	readContext, owner, err := s.beginPreparation(ctx)
	if err != nil {
		return protocol.Command{}, fileInputFailure(err)
	}
	defer s.finishPreparation(owner)
	draft := command.InputDraft()
	if draft.SkillIntent() != domain.InputSkillLiteral {
		skill, hasSkill, err := s.skills.Prepare(readContext, command.Text())
		if err != nil {
			return protocol.Command{}, fileInputFailure(err)
		}
		if !hasSkill && draft.SkillIntent() == domain.InputSkillRequired {
			return protocol.Command{}, fault.New(fault.CodeInvalidInput, "skill_not_found")
		}
		if hasSkill {
			command, err = command.WithSkillAttachment(skill)
			if err != nil {
				return protocol.Command{}, fault.Wrap(fault.CodeInvalidInput, "Skill attachment is invalid", err)
			}
		}
	}
	files, err := prepareWorkspaceFiles(readContext, draft.FileReferences(), s.workspace)
	if err != nil {
		return protocol.Command{}, fileInputFailure(err)
	}
	command, err = command.WithFileAttachments(files)
	if err != nil {
		return protocol.Command{}, fault.Wrap(fault.CodeInvalidInput, "file attachments are invalid", err)
	}
	if err = readContext.Err(); err != nil {
		return protocol.Command{}, err
	}
	return command, nil
}

func fileInputFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var fileError *domain.FileFailure
	if errors.As(err, &fileError) {
		return fault.New(fault.CodeInvalidInput, string(fileError.Kind()))
	}
	return err
}

// QueryFiles 只交换一个最新查询槽，并在锁外取消过期工作，不创建按键 goroutine。
func (s *workspaceInputSource) QueryFiles(query domain.FileSearchQuery) error {
	if err := query.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	if !s.started || s.closed {
		s.mu.Unlock()
		return domain.NewFileFailure(domain.FileUnavailable)
	}
	identity := query.Identity()
	if sameFileSearchSession(identity, s.cancelledIdentity) {
		s.mu.Unlock()
		return domain.NewFileFailure(domain.FileUnavailable)
	}
	if sameFileSearchSession(identity, s.latest.Identity()) && s.latest.Identity().RequestGeneration >= identity.RequestGeneration {
		if s.latest.Identity() != identity {
			s.mu.Unlock()
			return domain.NewFileFailure(domain.FileInvalid)
		}
	}
	if identity.WorkspaceGeneration == s.latest.Identity().WorkspaceGeneration && identity.SessionGeneration < s.latest.Identity().SessionGeneration {
		s.mu.Unlock()
		return domain.NewFileFailure(domain.FileInvalid)
	}
	cancelMatch := s.cancelMatch
	var cancelSearch context.CancelFunc
	if s.activeIdentity.WorkspaceGeneration != identity.WorkspaceGeneration || s.activeIdentity.SessionGeneration != identity.SessionGeneration {
		cancelSearch = s.cancelSearch
	}
	s.latest = query
	s.hasQuery = true
	s.mu.Unlock()
	if cancelMatch != nil {
		cancelMatch()
	}
	if cancelSearch != nil {
		cancelSearch()
	}
	s.notify()
	return nil
}

func (s *workspaceInputSource) CancelFiles(identity domain.FileSearchIdentity) {
	s.mu.Lock()
	if !s.hasQuery || s.latest.Identity().WorkspaceGeneration != identity.WorkspaceGeneration || s.latest.Identity().SessionGeneration != identity.SessionGeneration {
		s.mu.Unlock()
		return
	}
	s.hasQuery = false
	s.cancelledIdentity = identity
	cancelSearch, cancelMatch := s.cancelSearch, s.cancelMatch
	s.mu.Unlock()
	if cancelMatch != nil {
		cancelMatch()
	}
	if cancelSearch != nil {
		cancelSearch()
	}
	s.notify()
}
func (s *workspaceInputSource) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *workspaceInputSource) currentQuery() (domain.FileSearchQuery, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest, s.hasQuery && !s.closed
}
