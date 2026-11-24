package app

import (
	"context"
	"errors"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func newPickerExecutor(applicationConfig config.Config, paths dataPaths, cwd string, pluginDirs []string, environment *chatEnvironment) pickerExecutor {
	discovery := &pickerDiscovery{paths: paths, cwd: cwd}
	pluginDirs = append([]string(nil), pluginDirs...)
	return func(ctx, lifetime context.Context, request tui.PickerRequest) pickerExecution {
		result := tui.PickerResult{Request: request}
		if ctx.Err() != nil {
			result.Error = "Session picker operation cancelled"
			return pickerExecution{result: result}
		}
		if request.Operation == tui.PickerOpen {
			discovery.snapshot = nil
		}
		switch {
		case request.Operation == tui.PickerResume || (request.Operation == tui.PickerOpen && request.ThreadID.Valid()):
			return preparePickerCandidate(ctx, lifetime, request, applicationConfig, paths, cwd, pluginDirs, environment)
		case request.Operation == tui.PickerPreview:
			deps := defaultPreviewDependencies()
			if environment.roots != nil {
				deps.open = environment.borrowRepository(ctx)
				deps.closeRepository = keepBorrowedRepository
			}
			var err error
			result.Preview, result.Fatal, err = previewSession(ctx, paths.sessionRoot, request.ThreadID, deps)
			if err != nil {
				result.Error = "Session preview is unavailable"
			}
		default:
			if environment.roots != nil {
				deps := defaultSessionListDependencies()
				deps.openRepository = environment.borrowRepository(ctx)
				deps.closeRepository = keepBorrowedRepository
				discovery.dependencies = &deps
				defer func() { discovery.dependencies = nil }()
			}
			var err error
			result, err = discovery.list(ctx, request)
			if err != nil {
				result.Error = "Session discovery failed; refresh to retry"
			}
		}
		return pickerExecution{result: result}
	}
}

func preparePickerCandidate(
	ctx, lifetime context.Context,
	request tui.PickerRequest,
	applicationConfig config.Config,
	paths dataPaths,
	cwd string,
	pluginDirs []string,
	environment *chatEnvironment,
) pickerExecution {
	attempt := &recoveryAttempt{}
	execution := pickerExecution{result: tui.PickerResult{Request: request}, recovery: attempt}
	hooks := chatResourceHooks{
		interactiveApprovals: true, recovery: attempt, pluginDirs: pluginDirs, pluginCWD: cwd,
		environment: environment, lifetime: lifetime,
	}
	if request.Source.Valid() {
		hooks.expectedResumeCWD = cwd
	}
	resources, err := openChatResourcesWithHooks(ctx, applicationConfig, paths, string(request.ThreadID), false, cwd, hooks)
	if err != nil {
		execution.result.Error = pickerFailure(err)
		if errors.Is(err, errResumeDirectory) {
			execution.result.DirectoryMismatch = true
			execution.result.Error = "From the session's creation directory, run easycode --resume " +
				string(request.ThreadID) + " with compatible configuration"
		}
		execution.result.Fatal = attempt.cleanupErr != nil || (!request.Source.Valid() && attempt.fatal())
		return execution
	}
	return finishPickerCandidate(ctx, lifetime, execution, resources)
}

func newBranchPickerExecutor(applicationConfig config.Config, paths dataPaths, cwd string, pluginDirs []string, environment *chatEnvironment) pickerBranchExecutor {
	pluginDirs = append([]string(nil), pluginDirs...)
	return func(ctx, lifetime context.Context, request tui.PickerRequest, source *chatResources, admit func(domain.ThreadID) bool) pickerExecution {
		attempt := &recoveryAttempt{}
		execution := pickerExecution{result: tui.PickerResult{Request: request}, recovery: attempt}
		writer, ok := source.writer.(*session.JournalWriter)
		if !ok {
			execution.result.Error = "Source session journal is unavailable"
			return execution
		}
		target, err := domain.GenerateThreadID()
		if err != nil {
			execution.result.Error = "Session branch identity could not be generated"
			return execution
		}
		creation := &branchCreation{source: writer, target: target, title: request.Title, admit: admit}
		hooks := chatResourceHooks{
			interactiveApprovals: true, recovery: attempt, pluginDirs: pluginDirs, pluginCWD: cwd,
			environment: environment, lifetime: lifetime, expectedResumeCWD: cwd, branch: creation,
		}
		resources, err := openChatResourcesWithHooks(ctx, applicationConfig, paths, "", false, cwd, hooks)
		switch creation.publication {
		case session.BranchPublished:
			execution.result.BranchPublication = tui.BranchPublished
			execution.result.BranchThreadID = target
		case session.BranchPublicationUncertain:
			execution.result.BranchPublication = tui.BranchPublicationUncertain
			execution.result.BranchThreadID = target
		}
		if err != nil {
			execution.result.Error = pickerFailure(err)
			execution.result.Fatal = attempt.cleanupErr != nil || session.IsBranchSourceUnsafe(err)
			return execution
		}
		return finishPickerCandidate(ctx, lifetime, execution, resources)
	}
}

func finishPickerCandidate(ctx, lifetime context.Context, execution pickerExecution, resources *chatResources) pickerExecution {
	execution.resources = resources
	if ctx.Err() != nil {
		execution.result.Error = "Session recovery cancelled"
		if execution.result.Request.Operation == tui.PickerBranch {
			execution.result.Error = "Session branch cancelled"
		}
		return execution
	}
	model, err := assembleChatModel(lifetime, resources)
	if err == nil {
		model, err = model.LoadInitialTranscript(ctx)
	}
	if err != nil {
		execution.result.Error = "Target session could not be prepared"
		execution.result.Fatal = !execution.result.Request.Source.Valid() && execution.recovery.fatal()
		return execution
	}
	execution.result.Chat = &model
	return execution
}

func (e *chatEnvironment) borrowRepository(ctx context.Context) func(string) (*session.Repository, error) {
	return func(string) (*session.Repository, error) {
		if err := e.roots.layout.Check(ctx); err != nil {
			return nil, fault.New(fault.CodeSessionCatalog, "unsafe_storage_layout")
		}
		return e.roots.repository, nil
	}
}

func keepBorrowedRepository(*session.Repository) error { return nil }
