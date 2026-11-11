package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTimer struct {
	ticks    chan time.Time
	duration time.Duration
}

func (t *fakeTimer) Channel() <-chan time.Time { return t.ticks }
func (*fakeTimer) Stop()                       {}

type fakeClock struct{ timers chan *fakeTimer }

func (*fakeClock) Now() time.Time { return time.Unix(0, 0) }
func (c *fakeClock) Timer(d time.Duration) Timer {
	t := &fakeTimer{make(chan time.Time, 1), d}
	c.timers <- t
	return t
}

type exitValue struct {
	code int
	err  error
}
type fakeHandle struct {
	in            io.WriteCloser
	out, stderr   io.ReadCloser
	exits         chan exitValue
	mu            sync.Mutex
	alive, reaped bool
	signals       []bool
	waits         int
	stale         bool
	exit          exitValue
	closeOutput   func()
	waitEntered   chan struct{}
	waitRelease   <-chan struct{}
}

func (h *fakeHandle) Stdin() io.WriteCloser { return h.in }
func (h *fakeHandle) Stdout() io.ReadCloser { return h.out }
func (h *fakeHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *fakeHandle) ObserveExit() error    { h.exit = <-h.exits; return h.exit.err }
func (h *fakeHandle) Wait() (int, error) {
	if h.waitEntered != nil {
		close(h.waitEntered)
	}
	if h.waitRelease != nil {
		<-h.waitRelease
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.waits++
	h.reaped = true
	return h.exit.code, h.exit.err
}
func (h *fakeHandle) GroupAlive() (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.reaped {
		h.stale = true
		return false, errors.New("reused identity")
	}
	return h.alive, nil
}
func (h *fakeHandle) Terminate(force bool) error {
	h.mu.Lock()
	if h.reaped {
		h.stale = true
	}
	h.signals = append(h.signals, force)
	if force {
		h.alive = false
	}
	h.mu.Unlock()
	if force {
		if h.closeOutput != nil {
			h.closeOutput()
		}
		h.exits <- exitValue{code: 143}
	}
	return nil
}

type fakeLauncher struct {
	h     *fakeHandle
	calls int
}

func (l *fakeLauncher) Start(context.Context) (Handle, error) { l.calls++; return l.h, nil }
func fakeExited() *fakeHandle {
	h := &fakeHandle{out: io.NopCloser(strings.NewReader("stdout")), stderr: io.NopCloser(strings.NewReader("stderr")), exits: make(chan exitValue, 1)}
	h.exits <- exitValue{}
	return h
}

func TestOwnerIsPureSingleUseAndDrainsStreams(t *testing.T) {
	h := fakeExited()
	l := &fakeLauncher{h: h}
	var out, stderr bytes.Buffer
	o, err := New(Config{Launcher: l, Clock: WallClock{}, Stdout: &out, Stderr: &stderr, Timeout: time.Second})
	if err != nil || l.calls != 0 {
		t.Fatal("constructor launched")
	}
	r := o.Run(context.Background())
	<-o.Done()
	if !r.ExitKnown || r.Uncertain || out.String() != "stdout" || stderr.String() != "stderr" || h.waits != 1 || h.stale {
		t.Fatal("lost process resources", r)
	}
	if !o.Run(context.Background()).AlreadyAccepted || l.calls != 1 {
		t.Fatal("reexecuted")
	}
}
func TestOwnerCancelsBlockedStdinAndRetainsWaitOwnership(t *testing.T) {
	in, writer := io.Pipe()
	defer in.Close()
	out, outWriter := io.Pipe()
	stderr, errWriter := io.Pipe()
	entered, release := make(chan struct{}), make(chan struct{})
	h := &fakeHandle{in: writer, out: out, stderr: stderr, alive: true, exits: make(chan exitValue, 1), closeOutput: func() { outWriter.Close(); errWriter.Close() }, waitEntered: entered, waitRelease: release}
	c := &fakeClock{timers: make(chan *fakeTimer, 8)}
	o, _ := New(Config{Launcher: &fakeLauncher{h: h}, Clock: c, Input: []byte(strings.Repeat("x", 128<<10)), Stdout: io.Discard, Stderr: io.Discard, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() { done <- o.Run(ctx) }()
	<-c.timers
	cancel()
	grace := <-c.timers
	if grace.duration != 250*time.Millisecond {
		t.Fatal("missing escalation")
	}
	grace.ticks <- time.Unix(1, 0)
	<-entered
	select {
	case <-o.Done():
		t.Fatal("abandoned wait")
	default:
	}
	close(release)
	r := <-done
	<-o.Done()
	if !r.Cancelled || len(h.signals) != 2 || h.waits != 1 || h.stale {
		t.Fatal("bad cancellation", r)
	}
}
func TestOwnerReportsDetachedOutputWithoutLeakingReader(t *testing.T) {
	h := fakeExited()
	reader, writer := io.Pipe()
	defer writer.Close()
	h.out = reader
	c := &fakeClock{timers: make(chan *fakeTimer, 8)}
	o, _ := New(Config{Launcher: &fakeLauncher{h: h}, Clock: c, Stdout: io.Discard, Stderr: io.Discard, Timeout: time.Second})
	done := make(chan Result, 1)
	go func() { done <- o.Run(context.Background()) }()
	<-c.timers
	drain := <-c.timers
	drain.ticks <- time.Unix(1, 0)
	r := <-done
	<-o.Done()
	if !r.DrainExpired || !r.Uncertain || h.stale {
		t.Fatal("detached writer misreported", r)
	}
}

func TestOwnerTimeoutAndEarlyStdinCloseEscalateWithoutAbandoningOwner(t *testing.T) {
	for _, mode := range []string{"timeout", "closed-input"} {
		t.Run(mode, func(t *testing.T) {
			h := &fakeHandle{alive: true, exits: make(chan exitValue, 1), out: io.NopCloser(strings.NewReader("")), stderr: io.NopCloser(strings.NewReader(""))}
			var input []byte
			if mode == "closed-input" {
				reader, writer := io.Pipe()
				reader.Close()
				h.in = writer
				input = []byte("bounded input")
			}
			c := &fakeClock{timers: make(chan *fakeTimer, 8)}
			owner, _ := New(Config{Launcher: &fakeLauncher{h: h}, Clock: c, Input: input, Stdout: io.Discard, Stderr: io.Discard, Timeout: time.Second})
			done := make(chan Result, 1)
			go func() { done <- owner.Run(context.Background()) }()
			timeout := <-c.timers
			if timeout.duration != time.Second {
				t.Fatal("wrong execution budget")
			}
			if mode == "timeout" {
				timeout.ticks <- time.Unix(1, 0)
			}
			grace := <-c.timers
			if grace.duration != 250*time.Millisecond {
				t.Fatal("missing TERM grace")
			}
			grace.ticks <- time.Unix(2, 0)
			result := <-done
			<-owner.Done()
			if len(h.signals) != 2 || h.signals[0] || !h.signals[1] || h.waits != 1 || h.stale || result.Uncertain {
				t.Fatal("cleanup ownership failed", result)
			}
			if mode == "timeout" && !result.TimedOut || mode == "closed-input" && result.StdinError == nil {
				t.Fatal("lost failure classification", result)
			}
		})
	}
}

// simultaneousReader 在两条流都开始读取后才放行，确定性证明独立排空。
type simultaneousReader struct {
	reader  io.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *simultaneousReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return r.reader.Read(p)
}
func (*simultaneousReader) Close() error { return nil }
func TestOwnerDrainsFloodAndSplitUTF8Independently(t *testing.T) {
	data := bytes.Repeat([]byte("中"), 100000)
	release := make(chan struct{})
	out := &simultaneousReader{reader: bytes.NewReader(data), entered: make(chan struct{}), release: release}
	stderr := &simultaneousReader{reader: bytes.NewReader(data), entered: make(chan struct{}), release: release}
	h := fakeExited()
	h.out = out
	h.stderr = stderr
	var stdoutBytes, stderrBytes bytes.Buffer
	owner, _ := New(Config{Launcher: &fakeLauncher{h: h}, Clock: WallClock{}, Stdout: &stdoutBytes, Stderr: &stderrBytes, Timeout: time.Second})
	done := make(chan Result, 1)
	go func() { done <- owner.Run(context.Background()) }()
	<-out.entered
	<-stderr.entered
	close(release)
	result := <-done
	if result.Uncertain || !bytes.Equal(stdoutBytes.Bytes(), data) || !bytes.Equal(stderrBytes.Bytes(), data) {
		t.Fatal("flood lost raw bytes", result)
	}
}

// stoppedGroup 按探测阶段发布退出，强制 I/O 错误先于退出观察。
type stoppedGroup struct {
	*fakeHandle
	once           sync.Once
	signals        int
	initiallyAlive bool
	probes         int
}

func (h *stoppedGroup) GroupAlive() (bool, error) {
	h.probes++
	if h.initiallyAlive && h.probes == 1 {
		return true, nil
	}
	h.once.Do(func() { h.exits <- exitValue{code: 0} })
	return false, nil
}
func (h *stoppedGroup) Terminate(bool) error {
	h.signals++
	return errors.New("operation not permitted")
}

type rejectedOutput struct{}

func (rejectedOutput) Write([]byte) (int, error) { return 0, errors.New("bounded output rejected") }
func TestOwnerDoesNotSignalConfirmedStoppedGroupAfterIOFailure(t *testing.T) {
	for _, initiallyAlive := range []bool{false, true} {
		t.Run(map[bool]string{false: "already-stopped", true: "exits-before-signal"}[initiallyAlive], func(t *testing.T) {
			h := &stoppedGroup{initiallyAlive: initiallyAlive, fakeHandle: &fakeHandle{out: io.NopCloser(strings.NewReader("output")), stderr: io.NopCloser(strings.NewReader("")), exits: make(chan exitValue, 1)}}
			c := &fakeClock{timers: make(chan *fakeTimer, 8)}
			owner, err := New(Config{Launcher: fixedStoppedLauncher{h}, Clock: c, Stdout: rejectedOutput{}, Stderr: io.Discard, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan Result, 1)
			go func() { done <- owner.Run(context.Background()); close(done) }()
			<-c.timers
			next := <-c.timers
			// 假时钟必须释放已有 owner，业务断言在收口后执行。
			if next.duration == 250*time.Millisecond {
				next.ticks <- time.Unix(1, 0)
				next = <-c.timers
			}
			result := <-done
			<-owner.Done()
			wantSignals := 0
			if initiallyAlive {
				wantSignals = 1
			}
			if next.duration != time.Second || result.Uncertain || result.StdoutError == nil || !result.ExitKnown || h.signals != wantSignals || h.waits != 1 {
				t.Fatal("exit race lost confirmed cleanup", result, h.signals)
			}
		})
	}
}

type fixedStoppedLauncher struct{ handle Handle }

func (l fixedStoppedLauncher) Start(context.Context) (Handle, error) { return l.handle, nil }
