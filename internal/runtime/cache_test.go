package runtime

import (
	"context"
	"reflect"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func TestCacheObservationOnlyAfterDurableFinalize(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "sync failure"}[fail], func(t *testing.T) {
			conversation := &fakeConversation{stream: fixedStream(completedProviderEvent(t))}
			journal := &fakeJournal{}
			runtime := newTestRuntime(t, conversation, journal)
			checked := false
			journal.hook = func(drafts []session.RecordDraft) {
				for _, draft := range drafts {
					if draft.EventKind() == session.EventSampleUsage {
						checked = true
						if runtime.cache.snapshot.LastSample != nil || runtime.cache.snapshot.LastTurn != nil {
							t.Error("observation advanced before durable result")
						}
					}
				}
			}
			if fail {
				journal.failAt = 3
			}
			_, err := collectTurn(runtime, context.Background(), nil)
			if fail {
				if err == nil || runtime.cache.snapshot.LastSample != nil || runtime.cache.snapshot.LastTurn != nil {
					t.Fatal("Sync failure published success")
				}
				return
			}
			if err != nil || !checked || runtime.cache.snapshot.LastSample == nil || runtime.cache.snapshot.LastTurn == nil {
				t.Fatalf("missing durable observations: checked=%t err=%v", checked, err)
			}
			if runtime.cache.snapshot.LastSummary != nil {
				t.Fatal("sample counted as summary")
			}
			if err := runtime.cache.snapshot.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCacheQueryOwnerIsNonblockingAndOrdered(t *testing.T) {
	conversation := newControlledConversation()
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	done := startAgentLoop(t, loop)
	_ = submitLoopCommand(t, loop, "input", "work")
	invocation := receiveInvocation(t, conversation.calls)
	query := func(id protocol.RequestID) protocol.CacheSnapshotPayload {
		command, _ := protocol.NewQueryCacheCommand(id)
		result, err := loop.Submit(context.Background(), command)
		if err != nil || result.Disposition() != protocol.CommandDispositionQueried {
			t.Fatal(result, err)
		}
		accepted := false
		for item := range loop.Outputs() {
			if item.RequestID() != id {
				continue
			}
			if result, ok := item.CommandResult(); ok {
				accepted = result.Status() == protocol.CommandStatusAccepted
				continue
			}
			event, ok := item.ContextEvent()
			if !ok {
				continue
			}
			if !accepted {
				t.Fatal("snapshot preceded response")
			}
			p, err := protocol.DecodeCacheSnapshot(event)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		t.Fatal("snapshot missing")
		return protocol.CacheSnapshotPayload{}
	}
	first := query("cache-1")
	second := query("cache-2")
	if first.Activity != "turn" || first.LastSample != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("query changed observations")
	}
	invocation.release <- completedProviderEvent(t)
	receiveTurnEvent(t, loop.Outputs(), "input", protocol.EventTurnCompleted)
	third := query("cache-3")
	if third.LastSample == nil || third.LastTurn == nil || third.LastSummary != nil {
		t.Fatal("missing committed sample")
	}
	command, _ := protocol.NewQueryCacheCommand("cache-3")
	result, err := loop.Submit(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorDuplicateRequest)
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range loop.Outputs() {
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestCacheSummaryObservationDoesNotReplaceSample(t *testing.T) {
	conversation := newCompactionConversation()
	runtime := newTestRuntime(t, conversation, &fakeJournal{})
	initial, err := runtime.initialCacheSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	runtime.cache.snapshot = initial
	prior := protocol.UsagePayload{}
	runtime.cache.snapshot.LastTurn = &prior
	admission, code := runtime.preflightCompaction("")
	if code != "" {
		t.Fatal(code)
	}
	if err := runtime.runCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", admission, func(protocol.Event) {}); err != nil {
		t.Fatal(err)
	}
	if runtime.cache.snapshot.LastSummary == nil || runtime.cache.snapshot.LastSample != nil || runtime.cache.snapshot.LastTurn != &prior {
		t.Fatal("summary contaminated ordinary observations")
	}
}
func TestCacheFrozenPolicyMismatchFailsBeforeAppend(t *testing.T) {
	discarded := 0
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{"shape":"receipt-mismatch"}`))
	if err != nil {
		t.Fatal(err)
	}
	sample, err := provider.NewPreparedSample(envelope, testRuntimeUsage(t), syntheticRequestReceipt(domain.RequestSample), domain.EmptySamplePresentation(0), func() { t.Error("invalid sample finalized") }, func() { discarded++ })
	if err != nil {
		t.Fatal(err)
	}
	conversation := &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, sample))}
	journal := &fakeJournal{}
	config := testRuntimeConfig(t, journal)
	config.CachePolicy, _ = domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIHTTPSSE)
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = collectTurn(runtime, context.Background(), nil)
	if err == nil {
		t.Fatal("mismatched receipt accepted")
	}
	if discarded != 1 {
		t.Fatal("invalid receipt left prepared resources owned", discarded)
	}
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventSampleUsage {
				t.Fatal("invalid evidence became durable")
			}
		}
	}
}

var _ provider.Conversation = (*controlledConversation)(nil)

func TestCacheDiagnosticsRemainBoundedAndInstanceScoped(t *testing.T) {
	first := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{})
	second := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{})
	plan, err := first.contextPlan()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.freezeCacheSources(plan); err != nil {
		t.Fatal(err)
	}
	if err := second.freezeCacheSources(plan); err != nil {
		t.Fatal(err)
	}
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(5), domain.KnownUsageMetric(0), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(1), domain.UnknownUsageMetric())
	observation, err := first.cacheObservation(syntheticRequestReceipt(domain.RequestSample), usage, domain.RequestSample, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		first.commitSampleObservation(observation)
	}
	if first.cache.snapshot.LastSample == nil || first.cache.snapshot.LastSummary != nil || first.cache.snapshot.LastTurn != nil || second.cache.snapshot.LastSample != nil {
		t.Fatal("observations leaked across scope or instances")
	}
	encoded, err := codec.MarshalStable(first.cache.snapshot)
	if err != nil || len(encoded) > 64<<10 {
		t.Fatal("diagnostics are unbounded", err)
	}
	if err := first.cache.snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedSampleSurvivesLaterToolOutputFailure(t *testing.T) {
	executor := &recordingReadExecutor{}
	conversation := &fakeToolConversation{fakeConversation: &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-cache-failure"))))}}
	journal := &fakeJournal{failAt: 6}
	config := testRuntimeConfig(t, journal)
	config.ReadExecutor = executor
	config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = collectTurn(runtime, context.Background(), nil)
	if err == nil || runtime.cache.snapshot.LastSample == nil || runtime.cache.snapshot.LastTurn != nil || runtime.cache.snapshot.LastSummary != nil {
		t.Fatal("later failure erased durable sample or invented successful turn")
	}
	if executor.calls.Load() != 1 || conversation.calls.Load() != 1 {
		t.Fatal("failure repeated execution or generation")
	}
}

func TestUnexplainedCacheReadDropDoesNotInventTTLReason(t *testing.T) {
	runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{})
	plan, err := runtime.contextPlan()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.freezeCacheSources(plan); err != nil {
		t.Fatal(err)
	}
	commit := func(read domain.UsageMetric) {
		t.Helper()
		usage, err := domain.NewSampleUsage(domain.KnownUsageMetric(5), read, domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(1), domain.UnknownUsageMetric())
		if err != nil {
			t.Fatal(err)
		}
		observation, err := runtime.cacheObservation(syntheticRequestReceipt(domain.RequestSample), usage, domain.RequestSample, "")
		if err != nil {
			t.Fatal(err)
		}
		runtime.commitSampleObservation(observation)
	}
	commit(domain.KnownUsageMetric(7))
	commit(domain.KnownUsageMetric(0))
	if !runtime.cache.snapshot.UnexplainedReadDrop || runtime.cache.snapshot.ChangeReason != "unchanged" {
		t.Fatal("known read drop was not preserved as unexplained")
	}
	commit(domain.UnknownUsageMetric())
	if runtime.cache.snapshot.UnexplainedReadDrop || runtime.cache.snapshot.LastSample.ReadRatio != nil {
		t.Fatal("unknown read was treated as zero")
	}
}

func TestCacheDisplayConstraintsDoNotRejectValidModelRequests(t *testing.T) {
	config := testRuntimeConfig(t, &fakeJournal{})
	profile, err := contextplan.NewProviderProfile(domain.ProviderOpenAI, "/private/models/local")
	if err != nil {
		t.Fatal(err)
	}
	config.ContextProfile = profile
	runtime, err := New(&fakeConversation{stream: fixedStream(completedProviderEvent(t))}, config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = collectTurn(runtime, context.Background(), nil)
	if err != nil {
		t.Fatal("diagnostic label rejected an otherwise valid request", err)
	}
	if runtime.profile.Model() != "/private/models/local" || runtime.cache.snapshot.Model != protocol.CacheModelIdentity(runtime.profile.Model()) {
		t.Fatal("diagnostics changed model semantics or leaked a path")
	}
}
