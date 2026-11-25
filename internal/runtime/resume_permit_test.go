package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type gatedResumeInput struct {
	entered chan struct{}
	release chan struct{}
}

func (p gatedResumeInput) Prepare(ctx context.Context, command protocol.Command) (protocol.Command, error) {
	p.entered <- struct{}{}
	select {
	case <-p.release:
		return command, nil
	case <-ctx.Done():
		return protocol.Command{}, ctx.Err()
	}
}

func TestResumePermitSerializesPreparationAndDirectControls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conversation := newControlledConversation()
	journal := &fakeJournal{}
	value := newTestRuntime(t, conversation, journal)
	catalog, err := domain.NewSkillCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	preparation := gatedResumeInput{entered: make(chan struct{}, 2), release: make(chan struct{})}
	if err := value.InstallInputSources(catalog, preparation); err != nil {
		t.Fatal(err)
	}
	declaration, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "resume-check", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := domain.NewHookSnapshot([]domain.HookDeclaration{declaration}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.InstallHooks(hooks, &fakeHookRunner{}, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	value.hooks.trust[declaration.Reference.Key()] = protocol.HookTrustSession
	source := NewChatSession(value)
	if err := source.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		for range source.Outputs() {
		}
	}()
	t.Cleanup(func() {
		if err := source.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		<-outputDone
	})

	first, err := source.ReserveHandoff(ctx, HandoffBranch)
	if err != nil || !first.Matches(source, HandoffBranch) || first.Matches(source, HandoffResume) {
		t.Fatalf("idle source did not reserve: %v", err)
	}
	if duplicate, err := source.ReserveHandoff(ctx, HandoffResume); duplicate != nil || fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("repeated reservation: %v", err)
	}
	draft, err := domain.NewInteractiveInputDraft("Inspect the workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Append(ctx, draft); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("reserved append: %v", err)
	}
	if _, err := source.SetWorkMode(ctx, "reserved-work-mode", domain.WorkModePlan); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("reserved work mode change: %v", err)
	}
	select {
	case <-preparation.entered:
		t.Fatal("reserved source started input preparation")
	default:
	}
	if err := source.ResetHookTrust(ctx, declaration.Reference); err == nil {
		t.Fatal("direct hook reset bypassed the reservation")
	}
	target, err := protocol.NewHookTrustTarget(value.sessionID, value.threadID, declaration.Reference)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := protocol.NewResetHookTrustCommand("direct-reserved", target)
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.loop.Submit(ctx, direct)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorSessionBusy)
	if trust := value.hooks.trust[declaration.Reference.Key()]; trust != protocol.HookTrustSession {
		t.Fatalf("reserved hook reset changed trust: %s", trust)
	}
	result, err = source.RenameSession(ctx, "rename-reserved", "New title")
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorSessionBusy)
	if len(journal.snapshot()) != 0 {
		t.Fatal("reserved rename wrote to the source journal")
	}
	if result, err := source.QueryEffort(ctx, "query-reserved"); err != nil || result.Disposition() != protocol.CommandDispositionQueried {
		t.Fatalf("read-only command did not pass reservation: %v / %v", result.Status(), err)
	}
	if err := first.Release(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := source.ReserveHandoff(ctx, HandoffResume)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(ctx); err != nil || !second.Valid() {
		t.Fatalf("old release affected the current permit: %v", err)
	}
	if err := second.Release(ctx); err != nil {
		t.Fatal(err)
	}

	appendDone := make(chan error, 1)
	go func() {
		result, err := source.Append(ctx, draft)
		if err == nil && result.Disposition() != protocol.CommandDispositionStarting {
			err = errors.New("append did not start after permit release")
		}
		appendDone <- err
	}()
	select {
	case <-preparation.entered:
	case <-ctx.Done():
		t.Fatal("append failed to enter input preparation")
	}
	if held, err := source.ReserveHandoff(ctx, HandoffBranch); held != nil || fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("in-flight preparation did not prevent reservation: %v", err)
	}
	if _, err := source.SetWorkMode(ctx, "preparing-work-mode", domain.WorkModePlan); fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("work mode changed during preparation: %v", err)
	}
	close(preparation.release)
	if err := <-appendDone; err != nil {
		t.Fatal(err)
	}
	call := receiveInvocation(t, conversation.calls)
	if call.input != draft.Text() {
		t.Fatalf("original pending input did not reach Provider: %q", call.input)
	}
	if held, err := source.ReserveHandoff(ctx, HandoffBranch); held != nil || fault.Project(err).Code != fault.CodeSessionBusy {
		t.Fatalf("active worker did not prevent reservation: %v", err)
	}
	call.release <- completedProviderEvent(t)
}

func TestResumePermitOwnerCancellationAndFinishing(t *testing.T) {
	loop := newControlledAgentLoop(t, newControlledConversation(), &fakeJournal{})
	source := NewChatSession(loop.runtime)
	source.loop, source.started, source.interactive = loop, true, true
	lifetimeDone := make(chan struct{})
	source.lifetimeDone = lifetimeDone
	loop.state.Store(agentLoopRunning)
	close(loop.ready)
	owner := newAgentLoopRunState(loop, context.Background())
	defer func() {
		owner.clearResumePermit()
		close(loop.done)
	}()

	// 测试控制接收后的 owner 决定和结果投递，避免依赖调度时机。
	type reservationResult struct {
		permit *ResumePermit
		err    error
	}
	reserve := func() (resumeRequest, context.CancelFunc, <-chan reservationResult) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		t.Cleanup(cancel)
		results := make(chan reservationResult, 1)
		go func() {
			permit, err := source.ReserveHandoff(ctx, HandoffBranch)
			results <- reservationResult{permit, err}
		}()
		select {
		case request := <-loop.resume:
			return request, cancel, results
		case result := <-results:
			t.Fatalf("reservation ended before reaching the owner: %v", result.err)
			return resumeRequest{}, nil, nil
		}
	}
	request, cancel, done := reserve()
	cancel()
	request.result <- owner.handleResume(request)
	if result := <-done; result.permit != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("request cancelled before owner decision: %v", result.err)
	}

	owner.active = &activeLoopTurn{
		input:        queuedInput{requestID: "finishing"},
		terminalSeen: true,
		worker:       loopWorker{cancel: func() {}},
	}
	request, cancel, done = reserve()
	request.result <- owner.handleResume(request)
	if result := <-done; result.permit != nil || fault.Project(result.err).Code != fault.CodeSessionBusy {
		t.Fatalf("terminal publication admitted a reservation before cleanup: %v", result.err)
	}
	cancel()
	owner.handleTurnFinished(nil, true)

	request, cancel, done = reserve()
	accepted := owner.handleResume(request)
	cancel()
	request.result <- accepted
	result := <-done
	if result.err != nil || !result.permit.Valid() {
		t.Fatalf("owner-installed permit was lost after caller cancellation: %v", result.err)
	}
	// 父生命周期已结束时，子循环仍可能处于运行状态。
	close(lifetimeDone)
	if result.permit.Matches(source, HandoffBranch) {
		t.Fatal("branch permit matched after source lifetime cancellation")
	}
	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelRelease()
	released := make(chan error, 1)
	go func() { released <- result.permit.Release(releaseCtx) }()
	select {
	case request = <-loop.resume:
		request.result <- owner.handleResume(request)
	case err := <-released:
		t.Fatalf("release returned before owner confirmation: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	if result.permit.held.Load() || source.resumePermit != nil {
		t.Fatal("acknowledged release left the source reserved")
	}
}
