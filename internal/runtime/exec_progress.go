package runtime

import (
	"context"
	"sync"

	"easycode/internal/tool"
)

// executeWithProgress 由当前turn串行投影进度；executor回调只能更新有界内存快照。
// executor返回后先封闭回调入口再投影最后快照，禁止迟到或并发回调越过terminal。
func executeWithProgress(ctx context.Context, executor tool.ExecExecutor, candidate tool.ExecCandidate, emit func(tool.ExecProgress)) tool.InvocationResult {
	var mu sync.Mutex
	active, pending := true, false
	var latest tool.ExecProgress
	notify := make(chan struct{}, 1)
	finished := make(chan tool.InvocationResult, 1)
	submit := func(progress tool.ExecProgress) {
		if progress.Validate() != nil {
			return
		}
		mu.Lock()
		if !active {
			mu.Unlock()
			return
		}
		latest, pending = progress, true
		mu.Unlock()
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	flush := func(closeInput bool) {
		mu.Lock()
		if closeInput {
			active = false
		}
		value, available := latest, pending
		pending = false
		mu.Unlock()
		if available {
			emit(value)
		}
	}
	go func() { finished <- executor.Execute(ctx, candidate, submit) }()
	defer close(finished)
	for {
		select {
		case <-notify:
			flush(false)
		case result := <-finished:
			flush(true)
			return result
		}
	}
}
