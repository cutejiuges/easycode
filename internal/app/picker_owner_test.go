package app

import (
	"context"
	"easycode/internal/session"
	"easycode/internal/tui"
	"errors"
	"sync/atomic"
	"testing"
)

func TestPickerOwnerCoalescesAndWaitsForCancellation(t *testing.T) {
	entered := make(chan uint64, 4)
	release := make(chan struct{})
	cancelled := make(chan struct{})
	var active, maximum atomic.Int32
	owner := newPickerOwner(func(ctx context.Context, r tui.PickerRequest) pickerExecution {
		count := active.Add(1)
		maximum.Store(max(maximum.Load(), count))
		defer active.Add(-1)
		entered <- r.ID
		if r.ID == 1 {
			<-ctx.Done()
			close(cancelled)
			<-release
		}
		return pickerExecution{result: tui.PickerResult{Request: r}}
	})
	owner.Start(context.Background())
	owner.Submit(tui.PickerRequest{ID: 1, Operation: tui.PickerList})
	if <-entered != 1 {
		t.Fatal("first admission")
	}
	owner.Submit(tui.PickerRequest{ID: 2, Operation: tui.PickerList})
	<-cancelled
	owner.Submit(tui.PickerRequest{ID: 3, Operation: tui.PickerRefresh})
	close(release)
	if <-entered != 3 {
		t.Fatal("pending intent was not coalesced")
	}
	result := <-owner.Results()
	if result.Request.ID != 3 {
		t.Fatal("cancelled result published")
	}
	owner.Stop()
	owner.Stop()
	if err := owner.Wait(); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 1 {
		t.Fatal("overlapping operations")
	}
	if owner.Submit(tui.PickerRequest{ID: 4, Operation: tui.PickerList}) {
		t.Fatal("admission after stop")
	}
}
func TestPickerOwnerCancelledBeforeStart(t *testing.T) {
	var calls atomic.Int32
	owner := newPickerOwner(func(context.Context, tui.PickerRequest) pickerExecution { calls.Add(1); return pickerExecution{} })
	owner.Submit(tui.PickerRequest{ID: 1, Operation: tui.PickerList})
	owner.CancelPending()
	owner.Stop()
	owner.Start(context.Background())
	if err := owner.Wait(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("cancelled intent executed")
	}
}
func TestRecoveryAttemptStages(t *testing.T) {
	a := &recoveryAttempt{}
	if a.fatal() {
		t.Fatal("validation is retryable")
	}
	a.mutating()
	if !a.fatal() {
		t.Fatal("mutation not fatal")
	}
}

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
			owner := newPickerOwner(func(ctx context.Context, r tui.PickerRequest) pickerExecution {
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
	owner := newPickerOwner(func(ctx context.Context, r tui.PickerRequest) pickerExecution {
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

func TestPickerOwnerFatalFailureStopsAdmission(t *testing.T) {
	owner := newPickerOwner(func(ctx context.Context, r tui.PickerRequest) pickerExecution {
		return pickerExecution{result: tui.PickerResult{Request: r, Fatal: true, Error: "recovery uncertain"}}
	})
	owner.Start(context.Background())
	owner.Submit(tui.PickerRequest{ID: 1, Operation: tui.PickerList})
	result := <-owner.Results()
	if !result.Fatal {
		t.Fatal("fatal state lost")
	}
	if owner.Wait() == nil {
		t.Fatal("fatal completion not retained")
	}
	if owner.Submit(tui.PickerRequest{ID: 2, Operation: tui.PickerList}) {
		t.Fatal("fatal owner admitted another operation")
	}
}

func TestPickerOwnerRejectsCancelledHandoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := newPickerOwner(nil)
	_, release, admitted := owner.admit(ctx, 0)
	if !admitted {
		t.Fatal("admission failed")
	}
	defer release()
	// 不启动 worker，确定性覆盖取消已发生而 Stop 尚未执行的窗口。
	owner.offeredID = 1
	cancel()
	if owner.Accept(1) {
		t.Fatal("cancelled resources accepted")
	}
	if _, _, admitted = owner.admit(ctx, 0); admitted {
		t.Fatal("cancelled operation admitted")
	}
}
