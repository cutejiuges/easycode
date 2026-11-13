package plugin

import (
	"context"
	"easycode/internal/extension"
	"errors"
	"os"
	"sync"
)

// Options 的故障检查点只在显式 I/O 生命周期中调用，不在锁内执行。
type Options struct {
	Boundary    func(context.Context, *os.File) error
	BeforeOpen  func(string)
	AfterRead   func()
	BeforeClose func()
}

// Sources 是全部插件根和在途读取的唯一关闭 owner。
type Sources struct {
	mu      sync.Mutex
	closing bool
	loaded  bool
	roots   []*os.File
	active  map[*sourceHandle]struct{}
	drained chan struct{}
	done    chan struct{}
	result  error
	options Options
}

// NewSources 只分配固定完成信号和有界状态，不打开文件或启动 goroutine。
func NewSources(options Options) *Sources {
	return &Sources{active: make(map[*sourceHandle]struct{}), drained: make(chan struct{}), done: make(chan struct{}), options: options}
}

func (s *Sources) acquire() (*sourceHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, extension.SourceError("skill_unavailable")
	}
	if len(s.active) >= 16 {
		return nil, extension.SourceError("skill_source_busy")
	}
	h := &sourceHandle{owner: s, cancelled: make(chan struct{}), closeDone: make(chan struct{})}
	s.active[h] = struct{}{}
	return h, nil
}
func (s *Sources) release(h *sourceHandle) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, h)
	if s.closing && len(s.active) == 0 {
		close(s.drained)
	}
	return s.closing
}

// Close 的第一次接收启动唯一清理 worker；超时不会转移或放弃资源所有权。
func (s *Sources) Close(ctx context.Context) error {
	if ctx == nil {
		return extension.SourceError("skill_unavailable")
	}
	s.mu.Lock()
	start := !s.closing
	var active []*sourceHandle
	if start {
		s.closing = true
		for h := range s.active {
			active = append(active, h)
		}
		if len(active) == 0 {
			close(s.drained)
		}
	}
	s.mu.Unlock()
	if start {
		go s.cleanup(active)
	}
	select {
	case <-s.done:
		return s.result
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Sources) cleanup(active []*sourceHandle) {
	for _, h := range active {
		h.stop()
	}
	var result error
	for _, h := range active {
		result = errors.Join(result, h.forceClose())
	}
	<-s.drained
	for _, root := range s.roots {
		if err := root.Close(); err != nil {
			result = errors.Join(result, extension.SourceError("skill_source_close_failed"))
		}
	}
	s.result = result
	close(s.done)
}

// Done 仅在全部读取栈、文件关闭和原根关闭后触发。
func (s *Sources) Done() <-chan struct{} { return s.done }
