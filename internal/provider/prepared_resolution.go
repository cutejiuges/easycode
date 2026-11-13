package provider

import (
	"fmt"
	"sync/atomic"
)

// preparedResolution 只拥有一次内存提交的清理责任，不持有信封或采样策略。
type preparedResolution struct {
	finalize func()
	discard  func()
	state    atomic.Uint32
	resolved chan struct{}
}

func newPreparedResolution(finalize, discard func()) (*preparedResolution, error) {
	if finalize == nil || discard == nil {
		return nil, fmt.Errorf("native commit lifecycle callbacks are required")
	}
	return &preparedResolution{finalize: finalize, discard: discard, resolved: make(chan struct{})}, nil
}
func (r *preparedResolution) pending() bool   { return r != nil && r.state.Load() == 0 }
func (r *preparedResolution) finalized() bool { return r != nil && r.state.Load() == 1 }
func (r *preparedResolution) resolve(finalize bool) error {
	if r == nil || r.finalize == nil || r.discard == nil {
		return fmt.Errorf("native commit is invalid")
	}
	if !r.state.CompareAndSwap(0, 3) {
		<-r.resolved
		return fmt.Errorf("native commit is already resolved")
	}
	if finalize {
		r.finalize()
		r.state.Store(1)
	} else {
		r.discard()
		r.state.Store(2)
	}
	close(r.resolved)
	return nil
}
