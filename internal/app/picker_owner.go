package app

import (
	"context"
	"errors"
	"sync"

	"easycode/internal/domain"
	"easycode/internal/fault"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/tui"
)

type pickerExecution struct {
	result    tui.PickerResult
	resources *chatResources
	recovery  *recoveryAttempt
	draft     tui.RewindDraft
}

type pickerExecutor func(context.Context, context.Context, tui.PickerRequest) pickerExecution
type pickerBranchExecutor func(context.Context, context.Context, tui.PickerRequest, *chatResources, *rewindCandidate, func(domain.ThreadID) bool) pickerExecution
type pickerRewindExecutor func(context.Context, tui.PickerRequest, *chatResources) (tui.PickerResult, map[tui.RewindSelection]rewindCandidate)

// pickerOwner 串行处理终端请求，只有 worker 持有和关闭活动实例与待接管候选。
type pickerOwner struct {
	mu           sync.Mutex
	pending      *tui.PickerRequest
	cancelResume *tui.PickerRequest
	activeCancel context.CancelFunc
	activeDone   <-chan struct{}
	parentDone   <-chan struct{}
	generation   uint64
	latest       tui.PickerRequest
	offered      *pickerCandidate
	accepted     bool
	branchActive bool
	rewindReady  bool
	stopping     bool
	started      bool
	wake         chan struct{}
	results      chan tui.PickerResult
	done         chan struct{}
	execute      pickerExecutor
	branch       pickerBranchExecutor
	rewind       pickerRewindExecutor
	initial      *chatResources
	finalErr     error
	lastBranch   branchReport
}

type branchReport struct {
	requestID uint64
	notice    BranchNotice
}

type pickerCandidate struct {
	execution pickerExecution
	cancel    context.CancelFunc
	permit    *chatRuntime.ResumePermit
}

func newPickerOwner(execute pickerExecutor) *pickerOwner {
	return &pickerOwner{wake: make(chan struct{}, 1), results: make(chan tui.PickerResult, 1), done: make(chan struct{}), execute: execute}
}

func (o *pickerOwner) signal() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *pickerOwner) Submit(r tui.PickerRequest) bool {
	if r.ID == 0 || r.Operation < tui.PickerList || r.Operation > tui.PickerRewind ||
		((r.Operation == tui.PickerPreview || r.Operation == tui.PickerResume) && !r.ThreadID.Valid()) ||
		(r.Operation == tui.PickerOpen && !r.Source.Valid()) || (r.Source != "" && !r.Source.Valid()) ||
		(r.Operation != tui.PickerBranch && r.Title != "") {
		return false
	}
	if r.Operation == tui.PickerBranch &&
		(o.branch == nil || !r.Source.Valid() || r.ThreadID != "" || r.All || (r.Title != "" && r.Title.Validate() != nil)) {
		return false
	}
	if r.Operation == tui.PickerRewind && (o.rewind == nil || !r.Source.Valid() || r.ThreadID != "" || r.All) {
		return false
	}
	var emptySelection tui.RewindSelection
	historical := r.BrowseID != 0
	if historical {
		if r.Operation != tui.PickerBranch || r.Title != "" || !r.RewindSelection.TurnID.Valid() || !r.RewindSelection.InputID.Valid() {
			return false
		}
	} else if r.RewindSelection != emptySelection {
		return false
	}
	o.mu.Lock()
	if o.stopping || o.offered != nil || o.accepted || o.cancelResume != nil ||
		r.ID <= o.latest.ID || pickerCancelled(o.parentDone) {
		o.mu.Unlock()
		return false
	}
	if historical {
		if !o.rewindReady || o.latest.Operation != tui.PickerRewind || r.BrowseID != o.latest.ID || r.Source != o.latest.Source {
			o.mu.Unlock()
			return false
		}
	} else if o.branchActive {
		o.mu.Unlock()
		return false
	}
	o.latest = r
	o.generation++
	o.pending = &r
	o.branchActive = r.Operation == tui.PickerBranch || r.Operation == tui.PickerRewind
	o.rewindReady = false
	cancel := o.activeCancel
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.signal()
	return true
}

func (o *pickerOwner) Results() <-chan tui.PickerResult { return o.results }

// CancelPending 只废弃当前列表或预览请求，保留聊天内恢复许可。
func (o *pickerOwner) CancelPending() {
	o.mu.Lock()
	if o.offered != nil || o.accepted || o.branchActive || o.cancelResume != nil {
		o.mu.Unlock()
		return
	}
	o.generation++
	o.pending = nil
	cancel := o.activeCancel
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.signal()
}

func (o *pickerOwner) CancelResume(r tui.PickerRequest) {
	o.mu.Lock()
	if o.stopping || o.accepted || o.cancelResume != nil || r != o.latest {
		o.mu.Unlock()
		return
	}
	o.generation++
	o.pending = nil
	o.cancelResume = &r
	cancel := o.activeCancel
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.signal()
}

// Accept 固定候选并禁止取消回退，资源转移与关闭由 worker 完成。
func (o *pickerOwner) Accept(id uint64) bool {
	o.mu.Lock()
	candidate := o.offered
	if o.stopping || o.accepted || o.cancelResume != nil || candidate == nil ||
		candidate.execution.result.Request.ID != id || pickerCancelled(o.activeDone) || pickerCancelled(o.parentDone) {
		o.mu.Unlock()
		return false
	}
	if !chatLive(candidate.execution.resources) || (candidate.permit != nil && !candidate.permit.Valid()) {
		o.mu.Unlock()
		return false
	}
	o.accepted = true
	o.mu.Unlock()
	o.signal()
	return true
}

func (o *pickerOwner) Stop() {
	o.mu.Lock()
	o.generation++
	o.stopping = true
	o.pending = nil
	cancel := o.activeCancel
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.signal()
}

func (o *pickerOwner) Start(ctx context.Context) {
	done := ctx.Done()
	o.mu.Lock()
	if o.started {
		o.mu.Unlock()
		return
	}
	o.started = true
	o.parentDone = done
	o.mu.Unlock()
	go o.run(ctx)
}

func (o *pickerOwner) Wait() error { <-o.done; return o.finalErr }

func closePickerResources(resources *chatResources) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return resources.close(ctx)
}

type pickerRun struct {
	owner     *pickerOwner
	current   *chatResources
	candidate *pickerCandidate
	permit    *chatRuntime.ResumePermit
	recovery  *recoveryAttempt
	rewind    map[tui.RewindSelection]rewindCandidate
}

func (o *pickerOwner) run(ctx context.Context) {
	defer close(o.done)
	defer close(o.results)
	state := pickerRun{owner: o, current: o.initial}
	o.initial = nil
	defer func() {
		o.Stop()
		o.finalErr = errors.Join(o.finalErr, state.discardCandidate(), closePickerResources(state.current))
	}()
	for {
		o.mu.Lock()
		stopping, accepted := o.stopping, o.accepted
		generation, request, cancelled := o.generation, o.pending, o.cancelResume
		o.pending = nil
		o.mu.Unlock()
		// 交接外由活动界面投影控制流关闭，例如显示标题写入失败。
		handoff := state.permit != nil || request != nil || cancelled != nil
		switch {
		case stopping || ctx.Err() != nil:
			return
		case handoff && !chatLive(state.current):
			o.finalErr = errors.New("active session ended during terminal interaction")
			return
		case accepted:
			if !state.activate(ctx) {
				return
			}
		case cancelled != nil:
			if !state.returnToSource(ctx, pickerCancellation(*cancelled)) {
				return
			}
		case state.candidate != nil && !chatLive(state.candidate.execution.resources):
			if !state.returnToSource(ctx, tui.PickerResult{Request: state.candidate.execution.result.Request, Error: "Target session ended before handoff"}) {
				return
			}
		case request != nil:
			if !state.serve(ctx, generation, *request) {
				return
			}
		default:
			select {
			case <-ctx.Done():
				return
			case <-state.sourceHandoffDone():
			case <-state.candidateDone():
			case <-o.wake:
			}
		}
	}
}

func (s *pickerRun) sourceHandoffDone() <-chan struct{} {
	if s.permit == nil {
		return nil
	}
	return chatDone(s.current)
}

func (s *pickerRun) candidateDone() <-chan struct{} {
	if s.candidate == nil {
		return nil
	}
	return chatDone(s.candidate.execution.resources)
}

func chatDone(resources *chatResources) <-chan struct{} {
	if resources == nil || resources.session == nil {
		return nil
	}
	return resources.session.Done()
}

func chatLive(resources *chatResources) bool {
	return resources == nil || resources.session == nil || resources.session.Live()
}

func (s *pickerRun) discardCandidate() error {
	if s.candidate == nil {
		return nil
	}
	candidate := s.candidate
	s.candidate = nil
	candidate.cancel()
	return closePickerResources(candidate.execution.resources)
}

func (s *pickerRun) serve(ctx context.Context, generation uint64, request tui.PickerRequest) bool {
	o := s.owner
	operation, cancel, admitted := o.admit(ctx, generation)
	if !admitted {
		return true
	}
	offered := false
	defer func() {
		if !offered {
			cancel()
		}
	}()
	if request.Source.Valid() {
		if s.current == nil || request.Source != s.current.identity.ThreadID {
			if request.Operation == tui.PickerBranch || request.Operation == tui.PickerRewind {
				return s.returnToSource(ctx, tui.PickerResult{Request: request, Error: "Current session changed before branch preparation"})
			}
			return true
		}
		intent := chatRuntime.HandoffResume
		if request.Operation == tui.PickerBranch || request.Operation == tui.PickerRewind {
			intent = chatRuntime.HandoffBranch
		}
		if request.Operation == tui.PickerOpen || request.Operation == tui.PickerRewind ||
			(request.Operation == tui.PickerBranch && request.BrowseID == 0) {
			permit, err := s.current.session.ReserveHandoff(operation, intent)
			if err != nil {
				if operation.Err() != nil {
					return true
				}
				return s.returnToSource(ctx, tui.PickerResult{Request: request, Error: pickerFailure(err)})
			}
			s.permit = permit
		}
		if !s.permit.Matches(s.current.session, intent) || operation.Err() != nil {
			if operation.Err() == nil && request.BrowseID != 0 {
				return s.returnToSource(ctx, tui.PickerResult{Request: request, Error: "Historical branch selection expired; reopen rewind"})
			}
			return true
		}
		if request.ThreadID == request.Source {
			return s.returnToSource(ctx, tui.PickerResult{Request: request, Error: "This session is already active"})
		}
	} else if s.current != nil {
		return true
	}

	if request.Operation == tui.PickerRewind {
		return s.browseRewind(ctx, operation, generation, request)
	}
	lifetime, closeLifetime := context.WithCancel(ctx)
	var execution pickerExecution
	if request.Operation == tui.PickerBranch {
		execution = s.executeBranch(operation, lifetime, generation, request)
	} else {
		execution = o.execute(operation, lifetime, request)
	}
	s.recovery = execution.recovery
	if execution.resources == nil {
		closeLifetime()
	} else {
		execution.resources.cancelLifetime = closeLifetime
	}
	if execution.resources != nil && execution.result.Error == "" && !execution.result.Fatal {
		execution.result.Stage = tui.PickerPrepared
		candidate := &pickerCandidate{execution: execution, cancel: cancel, permit: s.permit}
		o.mu.Lock()
		offered = o.currentOperation(generation)
		if offered {
			o.offered = candidate
			s.candidate = candidate
		}
		o.mu.Unlock()
		if offered {
			s.publish(operation, execution.result)
			return true
		}
	}
	if err := closePickerResources(execution.resources); err != nil {
		o.finalErr = err
		return false
	}
	if execution.result.Fatal {
		o.finalErr = errors.New("session picker cleanup or recovery failed")
		if execution.recovery != nil {
			o.finalErr = errors.Join(o.finalErr, execution.recovery.cleanupErr)
		}
		s.publish(ctx, execution.result)
		return false
	}
	o.mu.Lock()
	valid := o.currentOperation(generation)
	o.mu.Unlock()
	if !valid {
		return true
	}
	if request.Source.Valid() && (request.Operation == tui.PickerBranch || request.Operation == tui.PickerResume ||
		(request.Operation == tui.PickerOpen && request.ThreadID.Valid())) {
		return s.returnToSource(ctx, execution.result)
	}
	s.publish(operation, execution.result)
	return true
}

func (s *pickerRun) browseRewind(ctx, operation context.Context, generation uint64, request tui.PickerRequest) bool {
	o := s.owner
	result, candidates := o.rewind(operation, request, s.current)
	if result.Request != request {
		return s.returnToSource(ctx, tui.PickerResult{Request: request, Error: "Historical branch selection expired; reopen rewind"})
	}
	if result.Fatal {
		o.finalErr = errors.New("source session history is invalid")
		s.publish(ctx, result)
		return false
	}
	o.mu.Lock()
	current := o.currentOperation(generation)
	if current {
		o.rewindReady = len(candidates) != 0
	}
	o.mu.Unlock()
	if current {
		s.rewind = candidates
		s.publish(operation, result)
	}
	return true
}

func (s *pickerRun) executeBranch(
	ctx, lifetime context.Context,
	generation uint64,
	request tui.PickerRequest,
) pickerExecution {
	o := s.owner
	source := s.current
	permit := s.permit
	var selection *rewindCandidate
	if request.BrowseID != 0 {
		chosen, ok := s.rewind[request.RewindSelection]
		if !ok {
			return pickerExecution{result: tui.PickerResult{Request: request, Error: "Historical branch selection expired; reopen rewind"}}
		}
		selection = &chosen
		s.rewind = nil
	}
	var admittedTarget domain.ThreadID
	execution := o.branch(ctx, lifetime, request, source, selection, func(target domain.ThreadID) bool {
		o.mu.Lock()
		defer o.mu.Unlock()
		if !o.currentOperation(generation) || o.latest != request || pickerCancelled(o.parentDone) ||
			!permit.Matches(source.session, chatRuntime.HandoffBranch) ||
			admittedTarget != "" || !target.Valid() || target == request.Source {
			return false
		}
		admittedTarget = target
		return true
	})
	result := &execution.result
	if result.Request != request || (result.BranchPublication != tui.BranchNotPublished &&
		(!admittedTarget.Valid() || result.BranchThreadID != admittedTarget)) ||
		(execution.resources != nil && (result.BranchPublication != tui.BranchPublished ||
			execution.resources.identity.ThreadID != admittedTarget ||
			execution.resources.identity.SessionID != source.identity.SessionID)) {
		*result = tui.PickerResult{Request: request, Error: "Session branch result did not match its attempt", Fatal: true}
		return execution
	}
	if result.BranchPublication != tui.BranchNotPublished {
		o.lastBranch = branchReport{
			requestID: request.ID,
			notice:    BranchNotice{threadID: admittedTarget, publication: result.BranchPublication},
		}
	}
	return execution
}

func pickerCancellation(request tui.PickerRequest) tui.PickerResult {
	message := "Resume cancelled"
	switch request.Operation {
	case tui.PickerBranch:
		message = "Session branch cancelled"
	case tui.PickerRewind:
		message = "Rewind cancelled"
	}
	return tui.PickerResult{Request: request, Error: message}
}

func (s *pickerRun) returnToSource(ctx context.Context, result tui.PickerResult) bool {
	o := s.owner
	o.mu.Lock()
	if o.accepted {
		o.mu.Unlock()
		return true
	}
	o.offered = nil
	o.mu.Unlock()
	if err := s.discardCandidate(); err != nil {
		o.finalErr = err
		return false
	}
	if !chatLive(s.current) {
		o.finalErr = errors.New("active session ended during resume")
		return false
	}
	if s.permit != nil {
		if err := s.permit.Release(context.Background()); err != nil {
			o.finalErr = err
			return false
		}
		s.permit = nil
	}
	s.rewind = nil
	if !chatLive(s.current) {
		o.finalErr = errors.New("active session ended during resume")
		return false
	}
	o.mu.Lock()
	if o.cancelResume != nil {
		result = pickerCancellation(*o.cancelResume)
		o.cancelResume = nil
	}
	o.branchActive = false
	o.rewindReady = false
	o.mu.Unlock()
	if result.Request.Operation == tui.PickerBranch {
		if retained := o.lastBranch; retained.requestID == result.Request.ID {
			result.BranchThreadID = retained.notice.threadID
			result.BranchPublication = retained.notice.publication
		}
	} else if s.recovery != nil {
		if s.recovery.uncertain {
			result.Error += ". Target recovery durability is uncertain."
		}
		if s.recovery.committed || s.recovery.artifacts.Durable {
			result.Error += " Completed target recovery changes were retained."
		}
		if s.recovery.artifacts.Changed && !s.recovery.artifacts.Durable {
			result.Error += " Target artifact maintenance may have made changes."
		}
	}
	s.recovery = nil
	result.Stage = tui.PickerCancelled
	s.publish(ctx, result)
	return true
}

func (s *pickerRun) activate(ctx context.Context) bool {
	o := s.owner
	candidate := s.candidate
	candidate.cancel()
	s.permit = nil
	source := s.current
	s.current = nil
	if err := closePickerResources(source); err != nil {
		o.finalErr = err
		return false
	}
	o.mu.Lock()
	stopping := o.stopping
	o.mu.Unlock()
	if stopping || ctx.Err() != nil {
		return false
	}
	if !chatLive(candidate.execution.resources) {
		o.finalErr = errors.New("target session ended during handoff")
		return false
	}
	s.current = candidate.execution.resources
	s.candidate = nil
	s.recovery = nil
	o.mu.Lock()
	o.offered = nil
	o.accepted = false
	o.branchActive = false
	o.rewindReady = false
	o.mu.Unlock()
	result := candidate.execution.result
	result.Stage = tui.PickerActivated
	result.RewindDraft = candidate.execution.draft
	s.publish(ctx, result)
	return true
}

func (s *pickerRun) publish(ctx context.Context, result tui.PickerResult) {
	select {
	case s.owner.results <- result:
	case <-ctx.Done():
	}
}

func (o *pickerOwner) currentOperation(generation uint64) bool {
	return !o.stopping && o.pending == nil && generation == o.generation && !pickerCancelled(o.activeDone)
}

// admit 将取消函数的所有权交给 owner；run 的唯一退出清理会调用 Stop。
func (o *pickerOwner) admit(ctx context.Context, generation uint64) (context.Context, context.CancelFunc, bool) {
	opCtx, cancel := context.WithCancel(ctx)
	done := opCtx.Done()
	o.mu.Lock()
	admitted := !o.stopping && o.pending == nil && generation == o.generation && !pickerCancelled(done)
	if admitted {
		o.activeCancel = cancel
		o.activeDone = done
	}
	o.mu.Unlock()
	if !admitted {
		cancel()
		return nil, nil, false
	}
	return opCtx, cancel, true
}

func pickerCancelled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func pickerFailure(err error) string {
	failure := fault.Project(err)
	return string(failure.Code) + ": " + failure.Message
}
