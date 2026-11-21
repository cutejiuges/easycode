package runtime

import (
	"context"

	"easycode/internal/protocol"
)

// loopWorker 把一次操作交给会话循环等待，关闭顺序与具体操作无关。
type loopWorker struct {
	cancelRequested bool
	cancel          context.CancelFunc
	events          <-chan runtimePublication
	finished        <-chan error
}

func startLoopWorker(ctx context.Context, runtime *Runtime, run func(context.Context, Emitter) error) loopWorker {
	operationCtx, cancel := context.WithCancel(ctx)
	events := make(chan runtimePublication)
	finished := make(chan error, 1)
	go func() {
		runtime.snapshotPublisher = func(publication runtimePublication) { events <- publication }
		err := run(operationCtx, func(event protocol.Event) { events <- runtime.publishContext(event) })
		runtime.snapshotPublisher = nil
		close(events)
		finished <- err
		close(finished)
	}()
	return loopWorker{cancel: cancel, events: events, finished: finished}
}

func (worker *loopWorker) requestCancel() bool {
	if worker.cancelRequested {
		return false
	}
	worker.cancelRequested = true
	worker.cancel()
	return true
}

func (worker *loopWorker) finish(open bool) bool {
	worker.finished = nil
	if open {
		worker.cancel()
	}
	return open
}
