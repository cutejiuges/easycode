package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/tool"
	"testing"
)

// executeExecActivityTest 为独占骨架单测显式安装调用组 owner，独立验证开始和持久终态。
func executeExecActivityTest(runtime *Runtime, ctx context.Context, emit Emitter, turn domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	runtime.activityEmitter = emit
	runtime.activityInvocations = map[tool.InvocationID]tool.Invocation{invocation.InvocationID(): invocation}
	defer func() { runtime.activityEmitter = nil; runtime.activityInvocations = nil }()
	return runtime.executeExecCall(ctx, emit, turn, invocation)
}

func eventKindsForTest(events []protocol.Event) []protocol.EventKind {
	kinds := make([]protocol.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

// validateToolActivitiesAndConversation 同时验证新增工具状态机，原有测试继续精确断言会话生命周期。
func validateToolActivitiesAndConversation(t *testing.T, events []protocol.Event) []protocol.Event {
	t.Helper()
	states := make(map[string]protocol.EventKind)
	var conversation []protocol.Event
	for _, event := range events {
		switch event.Kind {
		case protocol.EventToolReady, protocol.EventToolStarted, protocol.EventToolProgress, protocol.EventToolTerminal:
			payload, err := protocol.DecodeToolActivity(event)
			if err != nil || event.Validate() != nil {
				t.Fatal("invalid tool activity")
			}
			previous := states[payload.InvocationID]
			switch event.Kind {
			case protocol.EventToolReady:
				if previous != "" {
					t.Fatal("duplicate ready activity")
				}
			case protocol.EventToolStarted:
				if previous != protocol.EventToolReady {
					t.Fatal("started without ready")
				}
			case protocol.EventToolProgress:
				if previous != protocol.EventToolStarted && previous != protocol.EventToolProgress {
					t.Fatal("progress outside execution")
				}
			case protocol.EventToolTerminal:
				if previous == "" || previous == protocol.EventToolTerminal {
					t.Fatal("unpaired terminal activity")
				}
			}
			states[payload.InvocationID] = event.Kind
		default:
			conversation = append(conversation, event)
		}
	}
	return conversation
}
