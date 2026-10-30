package runtime

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/tool"
)

type heldTextCapture struct {
	entered  chan struct{}
	release  chan struct{}
	closed   chan struct{}
	atFinish bool
}

func (c *heldTextCapture) WriteText(string) error {
	if !c.atFinish {
		close(c.entered)
		<-c.release
	}
	return nil
}
func (c *heldTextCapture) Finish(bool, bool) tool.ArtifactSaving {
	if c.atFinish {
		close(c.entered)
		<-c.release
	}
	saving, _ := tool.NewArtifactSaving(tool.ArtifactUnavailable, "sync_failed", tool.ArtifactDescriptor{})
	return saving
}
func (c *heldTextCapture) Close() error { close(c.closed); return nil }

type heldTextStore struct{ capture *heldTextCapture }

func (s heldTextStore) StartTextCapture(context.Context, tool.InvocationID, tool.CapabilityID) (tool.TextOutputCapture, error) {
	return s.capture, nil
}

type capturedGlobExecutor struct{}

func (capturedGlobExecutor) Execute(ctx context.Context, inv tool.GlobInvocation, capture tool.TextOutputCapture) tool.SearchExecution {
	_ = capture.WriteText("a.txt\n")
	metadata, _ := tool.NewGlobResultMetadata([]string{"a.txt"}, false, 0, 1, tool.SearchComplete, tool.SearchSkipCounts{})
	preview, _ := tool.NewModelPreview("a.txt\n")
	result, _ := tool.NewGlobInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	execution, _ := tool.NewSearchExecution(result, false)
	return execution
}

func TestSearchOwnerRetainsCaptureAcrossCancellationAndBlockedPublication(t *testing.T) {
	for _, atFinish := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "finish"}[atFinish], func(t *testing.T) {
			capture := &heldTextCapture{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}), atFinish: atFinish}
			journal := &startedFailureJournal{failAt: 2}
			rt := newTestRuntime(t, &fakeToolConversation{fakeConversation: &fakeConversation{}}, journal)
			rt.textOutputStore = heldTextStore{capture}
			rt.globExecutor = capturedGlobExecutor{}
			input, _ := tool.NewGlobInput("*.txt", "", 10)
			ready, _ := tool.NewGlobReadyCall("search", input)
			id, _ := tool.GenerateInvocationID()
			inv, _ := tool.NewInvocation(id, ready)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := rt.executeToolCalls(ctx, runtimeTurnID, []tool.Invocation{inv}); done <- err }()
			<-capture.entered
			cancel()
			select {
			case <-capture.closed:
				t.Fatal("blocked capture was abandoned")
			default:
			}
			select {
			case <-done:
				t.Fatal("owner returned before blocked operation finished")
			default:
			}
			close(capture.release)
			if err := <-done; err == nil || errors.Is(err, context.Canceled) {
				t.Fatal("durable failure was hidden")
			}
			select {
			case <-capture.closed:
			default:
				t.Fatal("capture cleanup incomplete at return")
			}
		})
	}
}
