package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type budgetGateReader struct {
	entered  chan tool.ProviderCallID
	finished chan tool.ProviderCallID
	gates    map[tool.ProviderCallID]chan struct{}
}

func (r *budgetGateReader) Execute(_ context.Context, inv tool.ReadInvocation) tool.ReadExecution {
	r.entered <- inv.ProviderCallID()
	<-r.gates[inv.ProviderCallID()]
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = strings.Repeat("x", 400)
	}
	result := tool.RenderReadSuccess(inv, "same.txt", lines, 300)
	execution, _ := tool.NewReadExecution(result, tool.UnavailableReadObservation())
	r.finished <- inv.ProviderCallID()
	return execution
}

func TestOrderedBudgetIndependentOfEightWorkerCompletionOrder(t *testing.T) {
	var baseline []tool.InvocationResult
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			journal := &startedFailureJournal{}
			rt := newTestRuntime(t, &fakeToolConversation{fakeConversation: &fakeConversation{}}, journal)
			reader := &budgetGateReader{entered: make(chan tool.ProviderCallID, 8), finished: make(chan tool.ProviderCallID, 8), gates: make(map[tool.ProviderCallID]chan struct{})}
			rt.readExecutor = reader
			calls := make([]tool.Invocation, 8)
			for i := range calls {
				input, _ := tool.NewReadInput("same.txt", 1, 300)
				ready, _ := tool.NewReadReadyCall(tool.ProviderCallID(fmt.Sprintf("call-%d", i)), input)
				id, _ := tool.GenerateInvocationID()
				calls[i], _ = tool.NewInvocation(id, ready)
				reader.gates[ready.ProviderCallID()] = make(chan struct{})
			}
			type completion struct {
				results []tool.InvocationResult
				err     error
			}
			done := make(chan completion, 1)
			go func() {
				results, err := rt.executeToolCalls(context.Background(), runtimeTurnID, calls)
				done <- completion{results, err}
			}()
			for range calls {
				<-reader.entered
			}
			for i := range calls {
				index := i
				if reverse {
					index = 7 - i
				}
				id := calls[index].ProviderCallID()
				close(reader.gates[id])
				if <-reader.finished != id {
					t.Fatal("unexpected completion")
				}
			}
			outcome := <-done
			if outcome.err != nil {
				t.Fatal(outcome.err)
			}
			used, success, rejected := 0, 0, 0
			for i, result := range outcome.results {
				if result.ProviderCallID() != calls[i].ProviderCallID() {
					t.Fatal("result order changed")
				}
				used += result.Budget().ActualBytes()
				if result.Status() == tool.ResultSuccess {
					success++
				} else if result.Code() == "result_budget_exceeded" {
					rejected++
				} else {
					t.Fatalf("unexpected code %s", result.Code())
				}
				if reverse && (result.Preview() != baseline[i].Preview() || result.Budget() != baseline[i].Budget()) {
					t.Fatal("completion order changed budget")
				}
			}
			if used > tool.SamplePreviewBytes || success == 0 || rejected == 0 || used != rt.resultBudget.SampleUsed() {
				t.Fatal("sample budget or accounting violated")
			}
			resultIndex := 0
			for _, batch := range journal.batches {
				for _, draft := range batch {
					if draft.EventKind() == session.EventToolCallResult {
						if resultIndex >= 8 {
							t.Fatal("duplicate terminal")
						}
						resultIndex++
					}
				}
			}
			if resultIndex != 8 {
				t.Fatal("missing terminal fact")
			}
			if !reverse {
				baseline = outcome.results
			}
		})
	}
}

func TestBudgetExhaustionStopsNewAdmissionAndPreservesFrozenAccounting(t *testing.T) {
	rt := newTestRuntime(t, &fakeToolConversation{fakeConversation: &fakeConversation{}}, &startedFailureJournal{})
	rt.resultBudget = tool.NewResultBudget()
	// 以真实账本逐次扣满额度，最后一个额度只留下后续安全终态空间。
	for !rt.resultBudget.BodyExhausted() {
		if err := rt.resultBudget.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		allowance, err := rt.resultBudget.Allowance(tool.CapabilityRead)
		if err != nil {
			t.Fatal(err)
		}
		metadata, _ := tool.NewResultBudgetMetadata(allowance, allowance, tool.BudgetPreserved)
		if err := rt.resultBudget.Commit(tool.CapabilityRead, metadata); err != nil {
			t.Fatal(err)
		}
	}
	before := rt.resultBudget.TurnUsed()
	input, _ := tool.NewReadInput("never-open", 1, 1)
	ready, _ := tool.NewReadReadyCall("not-admitted", input)
	id, _ := tool.GenerateInvocationID()
	inv, _ := tool.NewInvocation(id, ready)
	var activities []protocol.Event
	results, err := rt.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{inv}, func(event protocol.Event) { activities = append(activities, event) })
	if err != nil || len(results) != 1 || results[0].Code() != "result_budget_exceeded" {
		t.Fatalf("admission: %v", err)
	}
	for _, event := range activities {
		if event.Kind == protocol.EventToolStarted {
			t.Fatal("exhausted call started")
		}
	}
	if !rt.resultBudget.BodyExhausted() || rt.resultBudget.TurnUsed() != before+results[0].Budget().ActualBytes() {
		t.Fatal("exhaustion latch or actual accounting changed")
	}
}

func TestBudgetRejectedReadInvalidatesPriorOverwriteEvidence(t *testing.T) {
	var table readObservations
	result, observation := completeObservation(t, "same.txt")
	table.beginGroup()
	table.stage(result, observation)
	table.activate()
	if table.lookup("same.txt").Kind() != tool.ReadObservationComplete {
		t.Fatal("initial evidence missing")
	}
	input, _ := tool.NewReadInput("same.txt", 1, 100)
	ready, _ := tool.NewReadReadyCall("later-read", input)
	id, _ := tool.GenerateInvocationID()
	inv, _ := tool.NewReadInvocation(id, ready)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 100)
	}
	large := tool.RenderReadSuccess(inv, "same.txt", lines, 100)
	evidence, _ := tool.NewFileBaseEvidence(tool.NewContentDigest([]byte("fixture")), 7, 0600, 1, 2)
	full, err := tool.NewCompleteReadObservation(large, evidence)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := tool.NewReadExecution(large, full)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := execution.Finalize(tool.SafetyResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	table.beginGroup()
	table.stage(finalized.Result(), finalized.Observation())
	table.activate()
	if finalized.Result().Code() != "result_budget_exceeded" || table.lookup("same.txt").Kind() != tool.ReadObservationUnavailable {
		t.Fatal("budget rejection preserved overwrite evidence")
	}
}

type exactBudgetReader struct{ sizes map[tool.ProviderCallID]int }

func (r exactBudgetReader) Execute(_ context.Context, inv tool.ReadInvocation) tool.ReadExecution {
	preview, _ := tool.NewModelPreview(strings.Repeat("x", r.sizes[inv.ProviderCallID()]))
	metadata, _ := tool.NewReadResultMetadata("budget.txt", inv.Input(), 1, 1, true, false, false)
	result, _ := tool.NewReadInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	execution, _ := tool.NewReadExecution(result, tool.UnavailableReadObservation())
	return execution
}

type budgetSteeringControl struct {
	claims    int
	inputs    []domain.UserInput
	committed int
}

func (c *budgetSteeringControl) claim(context.Context, domain.TurnID, uint32) ([]domain.UserInput, error) {
	c.claims++
	if c.claims == 1 {
		return c.inputs, nil
	}
	return nil, nil
}
func (c *budgetSteeringControl) resolve(_ domain.TurnID, inputs []domain.UserInput, state protocol.InputState, _ protocol.InputReason) {
	if state == protocol.InputCommitted {
		c.committed += len(inputs)
	}
}

func TestScriptedTurnExhaustionCommitsOutputsBeforeFailureAndStopsSteering(t *testing.T) {
	ledger := tool.NewResultBudget()
	sizes := make(map[tool.ProviderCallID]int)
	var prepared []*provider.PreparedSample
	total := 0
	for !ledger.BodyExhausted() {
		if err := ledger.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		allowance, err := ledger.Allowance(tool.CapabilityRead)
		if err != nil {
			t.Fatal(err)
		}
		id := tool.ProviderCallID(fmt.Sprintf("budget-%d", len(prepared)))
		input, _ := tool.NewReadInput("budget.txt", 1, 1)
		ready, _ := tool.NewReadReadyCall(id, input)
		sizes[id] = allowance
		total += allowance
		prepared = append(prepared, newPreparedSampleWithReady(t, ready))
		meta, _ := tool.NewResultBudgetMetadata(allowance, allowance, tool.BudgetPreserved)
		if err := ledger.Commit(tool.CapabilityRead, meta); err != nil {
			t.Fatal(err)
		}
	}
	base := &fakeConversation{}
	base.stream = func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		index := int(base.calls.Load()) - 1
		if index >= len(prepared) {
			return nil, fmt.Errorf("unexpected sample after budget exhaustion")
		}
		return fixedStream(completedProviderEventWithSample(t, prepared[index]))(context.Background(), provider.TurnInput{})
	}
	conversation := &fakeToolConversation{fakeConversation: base}
	journal := &fakeJournal{}
	rt := newTestRuntime(t, conversation, journal)
	rt.readExecutor = exactBudgetReader{sizes}
	steering := &budgetSteeringControl{}
	for i := 0; i < 2; i++ {
		input, err := domain.NewUserInput(domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%012d", i+2)), fmt.Sprintf("steer %d", i))
		if err != nil {
			t.Fatal(err)
		}
		steering.inputs = append(steering.inputs, input)
	}
	var events []protocol.Event
	err := rt.runTurn(context.Background(), runtimeInputs("start"), steering, func(event protocol.Event) { events = append(events, event) })
	if fault.Project(err).Code != fault.CodeToolResultBudgetExceeded {
		t.Fatalf("turn failure: %v", err)
	}
	if base.calls.Load() != int32(len(prepared)) || steering.claims != len(prepared)-1 || steering.committed != 3 || conversation.finalized.Load() != int32(len(prepared)) {
		t.Fatal("exhaustion reset budget, skipped outputs or claimed more input")
	}
	batches := journal.snapshot()
	if batches[len(batches)-1][0].EventKind() != session.EventTurnFailed || batches[len(batches)-2][0].EventKind() != session.EventProviderNativeCommit {
		t.Fatal("failure preceded native outputs")
	}
	actual := 0
	for _, batch := range batches {
		for _, draft := range batch {
			if draft.EventKind() == session.EventToolCallResult {
				payload, err := session.DecodeToolCallResultPayload(session.Record{EventKind: session.EventToolCallResult, PayloadVersion: session.EnvelopeVersion, Payload: draft.PayloadBytes()})
				if err != nil {
					t.Fatal(err)
				}
				actual += payload.Budget.ActualBytes()
			}
		}
	}
	if actual != total || actual > tool.TurnPreviewBytes {
		t.Fatal("steering reset or changed actual bytes")
	}
	last := events[len(events)-1]
	failure, err := protocol.DecodeTurnFailed(last)
	if err != nil || failure.Code != "tool_result_budget_exceeded" {
		t.Fatal("missing durable budget terminal")
	}
	t.Logf("samples=%d calls=%d preview_bytes=%d steering_inputs=2", len(prepared), len(prepared), actual)
}
