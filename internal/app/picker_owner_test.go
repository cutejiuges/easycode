package app

import (
	"context"
	"easycode/internal/session"
	"easycode/internal/tui"
	"errors"
	"sync/atomic"
	"testing"
)

type pickerJournal struct {
	closes atomic.Int32
	fail   bool
}

func (j *pickerJournal) AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error) {
	return nil, nil
}
func (j *pickerJournal) Poisoned() bool { return false }
func (j *pickerJournal) Close(context.Context) error {
	j.closes.Add(1)
	if j.fail {
		return errors.New("injected close failure")
	}
	return nil
}
func TestPickerOwnerResourceHandoffAndLateSuccess(t *testing.T) {
	for _, mode := range []string{"accepted", "late", "cleanup_failure"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			journal := &pickerJournal{fail: mode == "cleanup_failure"}
			owner := newPickerOwner(func(ctx, lifetime context.Context, r tui.PickerRequest) pickerExecution {
				close(entered)
				<-release
				return pickerExecution{result: tui.PickerResult{Request: r, Chat: &tui.Model{}}, resources: &chatResources{writer: journal}}
			})
			owner.Start(context.Background())
			owner.Submit(tui.PickerRequest{ID: 1, Operation: tui.PickerResume, ThreadID: "01900000-0000-7000-8000-000000000001"})
			<-entered
			if mode != "accepted" {
				owner.Stop()
			}
			close(release)
			if mode == "accepted" {
				result := <-owner.Results()
				if !owner.Accept(result.Request.ID) || owner.Accept(result.Request.ID) {
					t.Fatal("handoff must succeed exactly once")
				}
				if journal.closes.Load() != 0 {
					t.Fatal("accepted resource closed early")
				}
				owner.Stop()
			}
			err := owner.Wait()
			if (err != nil) != (mode == "cleanup_failure") {
				t.Fatal("wrong cleanup result", err)
			}
			if journal.closes.Load() != 1 {
				t.Fatal("resource not closed exactly once")
			}
		})
	}
}

func TestPickerOwnerParentCancellationWaitsForWorker(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	owner := newPickerOwner(func(ctx, lifetime context.Context, r tui.PickerRequest) pickerExecution {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return pickerExecution{result: tui.PickerResult{Request: r}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	owner.Start(ctx)
	owner.Submit(tui.PickerRequest{ID: 1, Operation: tui.PickerList})
	<-entered
	cancel()
	<-cancelled
	select {
	case <-owner.done:
		t.Fatal("owner abandoned worker before cleanup")
	default:
	}
	close(release)
	if err := owner.Wait(); err != nil {
		t.Fatal(err)
	}
}
