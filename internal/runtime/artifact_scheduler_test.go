package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type gatedArtifactExecutor struct {
	entered chan tool.InvocationID
	release <-chan struct{}
	mu      sync.Mutex
	calls   map[tool.InvocationID]int
}

func (e *gatedArtifactExecutor) Execute(ctx context.Context, v tool.ReadArtifactInvocation) tool.ReadArtifactExecution {
	e.mu.Lock()
	e.calls[v.InvocationID()]++
	e.mu.Unlock()
	e.entered <- v.InvocationID()
	select {
	case <-e.release:
	case <-ctx.Done():
	}
	status, code := tool.ResultError, "unavailable"
	if ctx.Err() != nil {
		status, code = tool.ResultCancelled, "cancelled"
	}
	p, _ := tool.NewModelPreview("ReadArtifact failed: " + code)
	r, _ := tool.NewReadArtifactInvocationResult(v, status, code, p, tool.ArtifactPage{})
	execution, _ := tool.NewReadArtifactFailure(r)
	return execution
}

func TestArtifactSchedulerBoundsWorkersAndPreservesResultOrder(t *testing.T) {
	for _, cancelActive := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelActive), func(t *testing.T) {
			release := make(chan struct{})
			executor := &gatedArtifactExecutor{entered: make(chan tool.InvocationID, 12), release: release, calls: make(map[tool.InvocationID]int)}
			journal := &fakeJournal{}
			runtime := &Runtime{journal: journal, tools: tool.Components{ReadArtifact: executor}}
			var invocations []tool.Invocation
			for i := 0; i < 12; i++ {
				id, _ := tool.GenerateInvocationID()
				input, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", uint64(i), 1)
				ready, _ := tool.NewReadArtifactReadyCall(tool.ProviderCallID(fmt.Sprintf("call-%d", i)), input)
				v, err := tool.NewInvocation(id, ready)
				requireNoError(t, err)
				invocations = append(invocations, v)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := executeReadyFixtureCalls(t, runtime, ctx, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdef"), invocations)
				done <- err
			}()
			for i := 0; i < maxParallelReadWorkers; i++ {
				<-executor.entered
			}
			select {
			case <-executor.entered:
				t.Fatal("artifact reads exceeded worker limit")
			default:
			}
			if cancelActive {
				cancel()
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			var results []tool.InvocationID
			for _, batch := range journal.snapshot() {
				for _, draft := range batch {
					if draft.EventKind() == session.EventToolCallResult {
						p, err := session.DecodeToolCallResultPayload(session.Record{EventKind: session.EventToolCallResult, Payload: draft.PayloadBytes()})
						requireNoError(t, err)
						results = append(results, p.InvocationID)
					}
				}
			}
			if len(results) != len(invocations) {
				t.Fatal("missing artifact results")
			}
			for i, v := range invocations {
				if results[i] != v.InvocationID() || executor.calls[v.InvocationID()] != 1 {
					t.Fatal("artifact pairing or at-most-once execution changed")
				}
			}
		})
	}
}
