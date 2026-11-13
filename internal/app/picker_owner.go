package app

import (
	"context"
	"errors"
	"sync"

	"easycode/internal/config"
	"easycode/internal/fault"
	"easycode/internal/tui"
)

type pickerExecution struct {
	result    tui.PickerResult
	resources *chatResources
}
type pickerExecutor func(context.Context, tui.PickerRequest) pickerExecution

// pickerOwner 使用一个 worker 和一个可覆盖的待处理槽，持有资源直到显式接收和最终关闭。
type pickerOwner struct {
	mu           sync.Mutex
	pending      *tui.PickerRequest
	activeCancel context.CancelFunc
	activeDone   <-chan struct{}
	parentDone   <-chan struct{}
	generation   uint64
	offeredID    uint64
	accepted     bool
	stopping     bool
	started      bool
	wake         chan struct{}
	results      chan tui.PickerResult
	done         chan struct{}
	execute      pickerExecutor
	finalErr     error
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
	if r.ID == 0 || r.Operation < tui.PickerList || r.Operation > tui.PickerResume || ((r.Operation == tui.PickerPreview || r.Operation == tui.PickerResume) && !r.ThreadID.Valid()) {
		return false
	}
	o.mu.Lock()
	if o.stopping || o.accepted || pickerCancelled(o.parentDone) {
		o.mu.Unlock()
		return false
	}
	o.generation++
	o.pending = &r
	cancel := o.activeCancel
	o.offeredID = 0
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.signal()
	return true
}
func (o *pickerOwner) Results() <-chan tui.PickerResult { return o.results }
func (o *pickerOwner) CancelPending() {
	o.mu.Lock()
	o.generation++
	o.pending = nil
	o.offeredID = 0
	cancel := o.activeCancel
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.signal()
}
func (o *pickerOwner) Accept(id uint64) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopping || o.accepted || o.offeredID != id || id == 0 || pickerCancelled(o.activeDone) || pickerCancelled(o.parentDone) {
		return false
	}
	o.accepted = true
	return true
}
func (o *pickerOwner) Stop() {
	o.mu.Lock()
	o.generation++
	o.stopping = true
	o.pending = nil
	o.offeredID = 0
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
func (o *pickerOwner) run(ctx context.Context) {
	defer close(o.done)
	defer close(o.results)
	var held *chatResources
	defer func() { o.Stop(); o.finalErr = errors.Join(o.finalErr, closePickerResources(held)) }()
	for {
		o.mu.Lock()
		stopping := o.stopping
		generation := o.generation
		request := o.pending
		o.pending = nil
		accepted := o.accepted
		offered := o.offeredID
		o.mu.Unlock()
		if stopping || ctx.Err() != nil {
			return
		}
		if held != nil && !accepted && offered == 0 {
			if err := closePickerResources(held); err != nil {
				o.finalErr = err
				return
			}
			held = nil
		}
		if request == nil {
			select {
			case <-ctx.Done():
				return
			case <-o.wake:
				continue
			}
		}
		opCtx, cancel, admitted := o.admit(ctx, generation)
		if !admitted {
			continue
		}
		execution := o.execute(opCtx, *request)
		o.mu.Lock()
		valid := !o.stopping && o.pending == nil && generation == o.generation && !pickerCancelled(o.activeDone)
		if valid && execution.resources != nil {
			o.offeredID = request.ID
		}
		o.mu.Unlock()
		if !valid {
			cancel()
			if err := closePickerResources(execution.resources); err != nil {
				o.finalErr = err
				return
			}
			if execution.result.Fatal {
				o.finalErr = errors.New("startup operation failed after mutation or cleanup")
				return
			}
			continue
		}
		if execution.result.Error != "" && execution.resources != nil {
			if err := closePickerResources(execution.resources); err != nil {
				execution.result.Fatal = true
			}
			execution.resources = nil
			o.mu.Lock()
			o.offeredID = 0
			o.mu.Unlock()
		}
		held = execution.resources
		if execution.result.Fatal {
			o.finalErr = errors.New("startup operation failed after mutation or cleanup")
		}
		select {
		case o.results <- execution.result:
		case <-opCtx.Done():
			if held != nil {
				cancel()
			}
		case <-ctx.Done():
			cancel()
			return
		}
		if held == nil {
			cancel()
		}
		if execution.result.Fatal {
			return
		}
	}
}
func newPickerExecutor(applicationConfig config.Config, paths dataPaths, cwd string, pluginDirs []string) pickerExecutor {
	discovery := &pickerDiscovery{paths: paths, cwd: cwd}
	pluginDirs = append([]string(nil), pluginDirs...)
	return func(ctx context.Context, request tui.PickerRequest) pickerExecution {
		result := tui.PickerResult{Request: request}
		if ctx.Err() != nil {
			result.Error = "Startup operation cancelled"
			return pickerExecution{result: result}
		}
		switch request.Operation {
		case tui.PickerList, tui.PickerRefresh:
			var err error
			result, err = discovery.list(ctx, request)
			if err != nil {
				result.Error = "Session discovery failed; refresh to retry"
			}
		case tui.PickerPreview:
			var err error
			result.Preview, result.Fatal, err = previewSession(ctx, paths.sessionRoot, request.ThreadID, defaultPreviewDependencies())
			if err != nil {
				result.Error = "Session preview is unavailable"
			}
		case tui.PickerResume:
			attempt := &recoveryAttempt{}
			resources, err := openChatResourcesWithHooks(ctx, applicationConfig, paths, string(request.ThreadID), false, cwd, chatResourceHooks{interactiveApprovals: true, recovery: attempt, pluginDirs: pluginDirs, pluginCWD: discovery.cwd})
			if err != nil {
				failure := fault.Project(err)
				result.Error = string(failure.Code) + ": " + failure.Message
				result.Fatal = attempt.fatal()
				return pickerExecution{result: result}
			}
			model, err := assembleChatModel(ctx, resources)
			if err != nil {
				result.Error = "Session startup failed"
				result.Fatal = attempt.fatal()
				return pickerExecution{result: result, resources: resources}
			}
			result.Chat = &model
			return pickerExecution{result: result, resources: resources}
		}
		return pickerExecution{result: result}
	}
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

// pickerCancelled 在接收线性化点只读取完成信号，不在锁内调用外部实现或阻塞。
func pickerCancelled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
