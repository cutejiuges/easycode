package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func TestRenameSessionAdmissionAndPoisoning(t *testing.T) {
	for _, stop := range []string{"caller", "host", "runtime", "active", "compacting", "queued", "poisoned"} {
		t.Run(stop, func(t *testing.T) {
			journal := &fakeJournal{}
			loop := newControlledAgentLoop(t, newControlledConversation(), journal)
			runCtx, cancelRun := context.WithCancel(context.Background())
			defer cancelRun()
			state := newAgentLoopRunState(loop, runCtx)
			state.title = "Previous title"
			caller, cancelCaller := context.WithCancel(context.Background())
			defer cancelCaller()
			hostStop := make(chan struct{})
			command := renameFixtureCommand(t, "rename", "New title")
			request := loopCommandRequest{command: command, result: make(chan loopCommandResponse, 1), cancelled: caller.Done(), hostStopped: hostStop}
			want := protocol.ControlErrorSessionClosing
			switch stop {
			case "caller":
				cancelCaller()
			case "host":
				close(hostStop)
			case "runtime":
				cancelRun()
			case "active":
				state.active = &activeLoopTurn{terminalSeen: true}
				want = protocol.ControlErrorSessionBusy
			case "compacting":
				state.compaction = &activeLoopCompaction{}
				want = protocol.ControlErrorSessionBusy
			case "queued":
				state.queue = append(state.queue, queuedInput{})
				want = protocol.ControlErrorSessionBusy
			case "poisoned":
				journal.failAt = 1
				want = protocol.ControlErrorSessionFailed
			}

			// 请求已从宿主送到 owner；这里刻意在调用者或 host 停止后处理。
			state.handleCommand(request)
			response := <-request.result
			if stop == "caller" {
				if fault.Project(response.err).Code != fault.CodeUserCancelled || len(loop.outputs) != 0 {
					t.Fatalf("cancelled handoff published an admission: %v", response.err)
				}
			} else {
				assertRejectedCode(t, response.result, want)
			}
			if len(journal.snapshot()) != 0 || state.title != "Previous title" {
				t.Fatal("rejected request changed the title")
			}
			if stop == "poisoned" {
				if state.mode != loopModeClosing || fault.Project(state.runErr).Code != fault.CodeSessionWrite {
					t.Fatalf("uncertain rename kept its writer active: %v", state.runErr)
				}
				input, err := protocol.NewSubmitInputCommand("after-failure", "next")
				requireNoError(t, err)
				state.handleCommand(loopCommandRequest{command: input, result: request.result})
				assertRejectedCode(t, (<-request.result).result, protocol.ControlErrorSessionClosing)
				if journal.calls != 1 {
					t.Fatalf("append calls after poison = %d", journal.calls)
				}
			} else if journal.calls != 0 {
				t.Fatal("rejected request reached the writer")
			}
		})
	}
}

func TestRenameSessionSyncPrecedesAcceptanceAndInput(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	journal := &fakeJournal{hook: func(batch []session.RecordDraft) {
		if batch[0].EventKind() == session.EventSessionTitleUpdated {
			close(entered)
			<-release
		}
	}}
	conversation := newControlledConversation()
	loop := newControlledAgentLoop(t, conversation, journal)
	done := startAgentLoop(t, loop)
	collected := make(chan []protocol.ControlItem, 1)
	go func() { collected <- drainControlItems(loop.Outputs()) }()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = loop.Stop(context.Background())
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	renamed := make(chan loopCommandResponse, 1)
	command := renameFixtureCommand(t, "rename", "Title @workspace")
	if _, err := loop.Submit(ctx, command); fault.Project(err).Code != fault.CodeInvalidInput {
		t.Fatalf("generic control accepted interactive-only rename: %v", err)
	}
	hostStop := make(chan struct{})
	go func() {
		result, err := loop.submit(ctx, command, hostStop)
		renamed <- loopCommandResponse{result: result, err: err}
	}()
	<-entered
	cancel()
	input, err := protocol.NewSubmitInputCommand("after-title", "hello")
	requireNoError(t, err)
	inputResult := make(chan loopCommandResponse, 1)
	go func() {
		result, err := loop.Submit(context.Background(), input)
		inputResult <- loopCommandResponse{result: result, err: err}
	}()
	select {
	case <-renamed:
		t.Fatal("received result before title Sync")
	case <-inputResult:
		t.Fatal("next turn started before title Sync")
	default:
	}
	close(release)
	if response := <-renamed; response.err != nil || response.result.Disposition() != protocol.CommandDispositionUpdated {
		t.Fatalf("accepted rename did not finish after cancellation: %v", response.err)
	}
	if response := <-inputResult; response.err != nil || response.result.Disposition() != protocol.CommandDispositionStarting {
		t.Fatalf("input did not follow rename: %v", response.err)
	}
	call := receiveInvocation(t, conversation.calls)
	if call.input != "hello" {
		t.Fatalf("model input = %q", call.input)
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	call.release <- completedProviderEvent(t)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items := <-collected
	if len(items) == 0 || items[0].RequestID() != command.RequestID() {
		t.Fatal("title confirmation was not ordered before other admission")
	}
	batches := journal.snapshot()
	if len(batches) < 2 || batches[0][0].EventKind() != session.EventSessionTitleUpdated || batches[1][0].EventKind() != session.EventTurnStarted {
		t.Fatal("title and turn records were out of order")
	}
}

func renameFixtureCommand(t *testing.T, id protocol.RequestID, title domain.SessionTitle) protocol.Command {
	t.Helper()
	command, err := protocol.NewRenameSessionCommand(id, title)
	requireNoError(t, err)
	return command
}
