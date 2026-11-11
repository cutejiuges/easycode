// Package process 提供与工具和 Hook 语义无关的单进程资源所有权。
package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// Failure 是平台 adapter 可以返回的安全基础设施分类。
type Failure string

const (
	Unsupported        Failure = "unsupported_platform"
	SandboxUnavailable Failure = "sandbox_unavailable"
)

func (f Failure) Error() string { return string(f) }

// Handle 保持原进程组身份至最终 Wait；适配器不得暴露 PID。
type Handle interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	ObserveExit() error
	Wait() (int, error)
	Terminate(bool) error
	GroupAlive() (bool, error)
}
type Launcher interface {
	Start(context.Context) (Handle, error)
}
type Timer interface {
	Channel() <-chan time.Time
	Stop()
}
type Clock interface {
	Now() time.Time
	Timer(time.Duration) Timer
}

// WallClock 在显式 Run 生命周期内提供真实计时。
type WallClock struct{}
type wallTimer struct{ timer *time.Timer }

func (WallClock) Now() time.Time              { return time.Now() }
func (WallClock) Timer(d time.Duration) Timer { return wallTimer{time.NewTimer(d)} }
func (t wallTimer) Channel() <-chan time.Time { return t.timer.C }
func (t wallTimer) Stop()                     { t.timer.Stop() }

// Config 的输出接收器分别只由一个 reader 调用；其清理属于外层业务 owner。
type Config struct {
	Launcher       Launcher
	Clock          Clock
	Input          []byte
	Stdout, Stderr io.Writer
	Timeout        time.Duration
}

// Result 区分已知退出、I/O 失败和资源状态，不解释业务输出。
type Result struct {
	Started, AlreadyAccepted                                    bool
	ExitCode                                                    int
	ExitKnown                                                   bool
	Cancelled, TimedOut, Uncertain, DrainExpired                bool
	StartError, WaitError, StdinError, StdoutError, StderrError error
}

// Owner 的 Done 只有在直接进程和全部 I/O goroutine 收口后关闭。
type Owner struct {
	config   Config
	mu       sync.Mutex
	accepted bool
	done     chan struct{}
}

func New(config Config) (*Owner, error) {
	if config.Launcher == nil || config.Clock == nil || config.Stdout == nil || config.Stderr == nil || config.Timeout <= 0 {
		return nil, errors.New("invalid process owner configuration")
	}
	config.Input = append([]byte(nil), config.Input...)
	return &Owner{config: config, done: make(chan struct{})}, nil
}
func (o *Owner) Done() <-chan struct{} { return o.done }

// Run 显式接收一次执行，取消和超时均不能移交或丢弃资源所有权。
func (o *Owner) Run(ctx context.Context) Result {
	o.mu.Lock()
	if o.accepted {
		o.mu.Unlock()
		return Result{AlreadyAccepted: true}
	}
	o.accepted = true
	o.mu.Unlock()
	defer close(o.done)
	var r Result
	if ctx == nil || ctx.Err() != nil {
		r.Cancelled = true
		return r
	}
	h, err := o.config.Launcher.Start(ctx)
	if err != nil {
		r.StartError = err
		return r
	}
	r.Started = true
	stdin, stdout, stderr := h.Stdin(), h.Stdout(), h.Stderr()
	if stdout == nil || stderr == nil {
		r.Uncertain = true
	}
	outDone, errDone := make(chan error, 1), make(chan error, 1)
	read := func(source io.ReadCloser, sink io.Writer, ch chan error) {
		if source == nil {
			ch <- errors.New("missing process output")
			return
		}
		_, err := io.Copy(sink, source)
		ch <- err
	}
	go read(stdout, o.config.Stdout, outDone)
	go read(stderr, o.config.Stderr, errDone)
	var inDone chan error
	if len(o.config.Input) > 0 {
		inDone = make(chan error, 1)
		go func() {
			if stdin == nil {
				inDone <- errors.New("missing process input")
				return
			}
			_, err := io.Copy(stdin, bytes.NewReader(o.config.Input))
			closeErr := stdin.Close()
			inDone <- errors.Join(err, closeErr)
		}()
	} else if stdin != nil {
		_ = stdin.Close()
	}
	exitDone := make(chan error, 1)
	go func() { exitDone <- h.ObserveExit() }()
	timeout := o.config.Clock.Timer(o.config.Timeout)
	defer timeout.Stop()
	outCh, errCh, inCh := (<-chan error)(outDone), (<-chan error)(errDone), (<-chan error)(inDone)
	observed := false
wait:
	for {
		select {
		case err := <-exitDone:
			observed = true
			r.Uncertain = r.Uncertain || err != nil
			break wait
		case <-ctx.Done():
			r.Cancelled = true
			break wait
		case <-timeout.Channel():
			r.TimedOut = true
			break wait
		case r.StdoutError = <-outCh:
			outCh = nil
			if r.StdoutError != nil {
				break wait
			}
		case r.StderrError = <-errCh:
			errCh = nil
			if r.StderrError != nil {
				break wait
			}
		case r.StdinError = <-inCh:
			inCh = nil
			if r.StdinError != nil {
				break wait
			}
		}
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	alive, probeErr := h.GroupAlive()
	r.Uncertain = r.Uncertain || probeErr != nil
	// 已确认停止的原组无需再发信号，避免 Darwin 对僵尸组返回 EPERM 而伪造清理不确定。
	if alive || probeErr != nil {
		// 信号请求可能与自然退出并发；清理事实由原组探测、最终 Wait 和流收口确认。
		_ = h.Terminate(false)
		grace := o.config.Clock.Timer(250 * time.Millisecond)
		<-grace.Channel()
		grace.Stop()
		alive, probeErr = h.GroupAlive()
		r.Uncertain = r.Uncertain || probeErr != nil
		if alive || probeErr != nil {
			_ = h.Terminate(true)
		}
	}
	if !observed {
		r.Uncertain = (<-exitDone) != nil || r.Uncertain
	}
	close(exitDone)
	for attempt := 0; ; attempt++ {
		alive, probeErr = h.GroupAlive()
		if probeErr != nil || !alive || attempt == 40 {
			r.Uncertain = r.Uncertain || alive || probeErr != nil
			break
		}
		settle := o.config.Clock.Timer(25 * time.Millisecond)
		<-settle.Channel()
		settle.Stop()
	}
	r.ExitCode, r.WaitError = h.Wait()
	r.ExitKnown = r.WaitError == nil
	r.Uncertain = r.Uncertain || r.WaitError != nil && !errors.Is(r.WaitError, SandboxUnavailable)
	drain := o.config.Clock.Timer(time.Second)
	for outCh != nil || errCh != nil || inCh != nil {
		select {
		case r.StdoutError = <-outCh:
			outCh = nil
		case r.StderrError = <-errCh:
			errCh = nil
		case r.StdinError = <-inCh:
			inCh = nil
		case <-drain.Channel():
			r.DrainExpired = true
			if stdout != nil {
				_ = stdout.Close()
			}
			if stderr != nil {
				_ = stderr.Close()
			}
			if stdin != nil {
				_ = stdin.Close()
			}
			if outCh != nil {
				r.StdoutError = <-outCh
				outCh = nil
			}
			if errCh != nil {
				r.StderrError = <-errCh
				errCh = nil
			}
			if inCh != nil {
				r.StdinError = <-inCh
				inCh = nil
			}
		}
	}
	drain.Stop()
	close(outDone)
	close(errDone)
	if inDone != nil {
		close(inDone)
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if stderr != nil {
		_ = stderr.Close()
	}
	r.Uncertain = r.Uncertain || r.DrainExpired
	return r
}
