package runtime

import (
	"context"
	"fmt"
	"strings"
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

type artifactBarrierOperations struct{ trace []string }

func (o *artifactBarrierOperations) executeApproved(_ context.Context, _ Emitter, _ domain.TurnID, v tool.Invocation) (tool.InvocationResult, bool, error) {
	o.trace = append(o.trace, "exclusive:"+string(v.Capability()))
	return tool.NewInvocationErrorResult(v, tool.ResultDenied, "approval_required", "Tool denied"), false, nil
}
func (o *artifactBarrierOperations) executeReadBatch(_ context.Context, _ domain.TurnID, values []tool.Invocation) ([]tool.InvocationResult, error) {
	var names []string
	var results []tool.InvocationResult
	for _, v := range values {
		names = append(names, string(v.Capability()))
		results = append(results, tool.NewInvocationErrorResult(v, tool.ResultError, "unavailable", "Tool unavailable"))
	}
	o.trace = append(o.trace, "read:"+strings.Join(names, ","))
	return results, nil
}
func (*artifactBarrierOperations) persistToolResult(context.Context, domain.TurnID, *tool.InvocationResult) error {
	return nil
}

func TestArtifactMixedSchedulerHonorsEveryExclusiveBarrier(t *testing.T) {
	artifactInput, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", 0, 3)
	artifactCall, _ := tool.NewReadArtifactReadyCall("artifact", artifactInput)
	readInput, _ := tool.NewReadInput("README.md", 1, 1)
	readCall, _ := tool.NewReadReadyCall("read", readInput)
	patch, _ := runtimePatchInvocation(t)
	write, _ := runtimeWriteInvocation(t)
	execInput, _ := tool.NewExecInput("true", "", 0)
	execCall, _ := tool.NewExecReadyCall("exec", execInput)
	makeInvocation := func(call tool.ReadyCall) tool.Invocation {
		id, _ := tool.GenerateInvocationID()
		v, err := tool.NewInvocation(id, call)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	values := []tool.Invocation{makeInvocation(artifactCall), makeInvocation(readCall), patch, write, makeInvocation(execCall), makeInvocation(artifactCall)}
	operations := &artifactBarrierOperations{}
	observations := &readObservations{}
	results, err := (orderedToolScheduler{operations: operations, observations: observations}).execute(context.Background(), domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdef"), values)
	if err != nil || len(results) != len(values) {
		t.Fatalf("mixed scheduling: %v", err)
	}
	want := "read:artifact.read,fs.read|exclusive:fs.patch|exclusive:fs.write|exclusive:process.exec|read:artifact.read"
	if strings.Join(operations.trace, "|") != want {
		t.Fatalf("barriers: %v", operations.trace)
	}
	for i, result := range results {
		if result.InvocationID() != values[i].InvocationID() || result.Validate() != nil {
			t.Fatal("mixed output pairing changed")
		}
	}
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
			runtime := &Runtime{journal: journal, readArtifactExecutor: executor}
			var invocations []tool.Invocation
			for i := 0; i < 12; i++ {
				id, _ := tool.GenerateInvocationID()
				input, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", uint64(i), 1)
				ready, _ := tool.NewReadArtifactReadyCall(tool.ProviderCallID(fmt.Sprintf("call-%d", i)), input)
				v, err := tool.NewInvocation(id, ready)
				if err != nil {
					t.Fatal(err)
				}
				invocations = append(invocations, v)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := runtime.executeToolCalls(ctx, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdef"), invocations)
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
						p, err := session.DecodeToolCallResultPayload(session.Record{EventKind: session.EventToolCallResult, PayloadVersion: session.EnvelopeVersion, Payload: draft.PayloadBytes()})
						if err != nil {
							t.Fatal(err)
						}
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
