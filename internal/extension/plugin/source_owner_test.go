//go:build darwin || linux

package plugin

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSourceBorrowLimitAndCloseGate(t *testing.T) {
	s, _, cs := load(t, Options{}, fixture(t, "claude"))
	var handles []*sourceHandle
	for i := 0; i < 16; i++ {
		h, err := cs[0].Opener.Open(context.Background(), "skills/review")
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, h.(*sourceHandle))
	}
	if _, err := cs[0].Opener.Open(context.Background(), "skills/review"); err == nil || err.Error() != "skill_source_busy" {
		t.Fatalf("borrow limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := cs[0].Opener.Open(context.Background(), "skills/review"); err == nil || err.Error() != "skill_unavailable" {
		t.Fatal("close gate", err)
	}
	select {
	case <-s.Done():
		t.Fatal("closed roots with outstanding handles")
	default:
	}
	for _, h := range handles {
		_ = h.Close()
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, root := range s.roots {
		if _, err := root.Stat(); err == nil {
			t.Fatal("root retained after Done")
		}
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCloseTimeoutKeepsReadStackAndBlockedFileCloseOwned(t *testing.T) {
	reading, resumeRead := make(chan struct{}), make(chan struct{})
	closing, resumeClose := make(chan struct{}), make(chan struct{})
	var block atomic.Bool
	var readOnce, closeOnce sync.Once
	s, _, cs := load(t, Options{
		AfterRead: func() {
			if block.Load() {
				readOnce.Do(func() { close(reading) })
				<-resumeRead
			}
		},
		BeforeClose: func() {
			if block.Load() {
				closeOnce.Do(func() { close(closing) })
				<-resumeClose
			}
		},
	}, fixture(t, "claude"))
	h, err := cs[0].Opener.Open(context.Background(), "skills/review")
	if err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	finished := make(chan error, 1)
	go func() {
		_, readErr := h.Read(context.Background(), "SKILL.md", 64<<10)
		closeErr := h.Close()
		finished <- errors.Join(readErr, closeErr)
	}()
	<-reading
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-closing
	for i := 0; i < 3; i++ {
		if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	select {
	case <-s.Done():
		t.Fatal("Done before read stack and close completed")
	default:
	}
	for _, root := range s.roots {
		if _, err := root.Stat(); err != nil {
			t.Fatal("root closed early")
		}
	}
	close(resumeRead)
	select {
	case <-s.Done():
		t.Fatal("Done before blocked close completed")
	default:
	}
	close(resumeClose)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("read accepted after source cancellation", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("missing final completion")
	}
}
