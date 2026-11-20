package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"

	"github.com/coder/websocket"
)

func TestWebSocketDurableIncrementalAndReset(t *testing.T) {
	requests := make(chan []byte, 8)
	var connects atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer ws-test" || r.Header.Get("Sec-WebSocket-Extensions") != "" {
			t.Error("unexpected websocket handshake")
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		generation := connects.Add(1)
		for n := 1; ; n++ {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			requests <- body
			id := fmt.Sprintf("resp-%d-%d", generation, n)
			for _, event := range []string{fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id), `{"type":"response.output_item.done","item":{"type":"message","id":"msg","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"answer"}],"future":{"opaque":"preserved"}}}`, fmt.Sprintf(`{"type":"response.completed","response":{"id":%q}}`, id)} {
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIWebSocketAuto)
	settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
	instance, err := New(Config{Inference: settings, BaseURL: server.URL + "/v1/", APIKey: secret.New("ws-test"), Model: "gpt-test", CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	firstInput := testOpenAITurnInput(t, conversation, "first")
	firstFull, _ := conversation.compileSamplingRequest(conversation.history.Entries(), firstInput)
	stream, err := conversation.Stream(context.Background(), firstInput)
	if err != nil {
		t.Fatal(err)
	}
	first := readOpenAITerminal(t, conversation, stream)
	if first.Kind() != provider.StreamEventCompleted {
		t.Fatal(first.Error())
	}
	receipt, _ := first.PreparedSample().Receipt()
	if receipt.Transport != domain.TransportWebSocket || receipt.Mode != domain.RequestFull {
		t.Fatal(receipt)
	}
	wire1 := <-requests
	goldenPath := "testdata/reasoning_websocket_full.golden.json"
	golden, err := os.ReadFile(goldenPath)
	if err != nil || !bytes.Equal(wire1, bytes.TrimSpace(golden)) {
		t.Fatalf("full wire golden mismatch: %v", err)
	}
	fullWire, _ := websocketBody(firstFull, "", 0)
	if !bytes.Equal(wire1, fullWire.Bytes()) {
		t.Fatal("first request is not full canonical envelope")
	}
	if conversation.wsResponseID != "" {
		t.Fatal("baseline advanced before durable finalize")
	}
	if err := first.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
	secondInput := testOpenAITurnInput(t, conversation, "second")
	full, err := conversation.compileSamplingRequest(conversation.history.Entries(), secondInput)
	if err != nil {
		t.Fatal(err)
	}
	stream, err = conversation.Stream(context.Background(), secondInput)
	if err != nil {
		t.Fatal(err)
	}
	second := readOpenAITerminal(t, conversation, stream)
	if second.Kind() != provider.StreamEventCompleted {
		t.Fatal(second.Error())
	}
	wire2 := <-requests
	goldenPath = "testdata/reasoning_websocket_incremental.golden.json"
	golden, err = os.ReadFile(goldenPath)
	if err != nil || !bytes.Equal(wire2, bytes.TrimSpace(golden)) {
		t.Fatalf("incremental wire golden mismatch: %v", err)
	}
	var incremental websocketCreate
	if err := codec.Unmarshal(wire2, &incremental); err != nil {
		t.Fatal(err)
	}
	if incremental.PreviousResponseID != "resp-1-1" || len(incremental.Input) != 1 || incremental.Store {
		t.Fatalf("bad suffix: %s", wire2)
	}
	var original responsesRequest
	_ = codec.Unmarshal(full.Bytes(), &original)
	var previous websocketCreate
	_ = codec.Unmarshal(wire1, &previous)
	reconstructed := append(append(previous.Input, conversation.historySnapshot()[1].Outputs...), incremental.Input...)
	rebuilt, _ := codec.MarshalCanonical(reconstructed, maxResponsesRequestBytes)
	expected, _ := codec.MarshalCanonical(original.Input, maxResponsesRequestBytes)
	if !bytes.Equal(rebuilt.Bytes(), expected.Bytes()) {
		t.Fatal("incremental prefix + outputs + suffix differs from logical full request")
	}
	receipt, _ = second.PreparedSample().Receipt()
	if receipt.Mode != domain.RequestIncremental || receipt.Reason != domain.ResetNone {
		t.Fatal(receipt)
	}
	if err := second.PreparedSample().Discard(); err != nil {
		t.Fatal(err)
	}
	thirdInput := testOpenAITurnInput(t, conversation, "third")
	stream, err = conversation.Stream(context.Background(), thirdInput)
	if err != nil {
		t.Fatal(err)
	}
	third := readOpenAITerminal(t, conversation, stream)
	if third.Kind() != provider.StreamEventCompleted {
		t.Fatal(third.Error())
	}
	var reset websocketCreate
	_ = codec.Unmarshal(<-requests, &reset)
	if reset.PreviousResponseID != "" || connects.Load() != 2 {
		t.Fatal("discard did not reset connection")
	}
	_ = third.PreparedSample().Finalize()
	restoredInput := testOpenAITurnInput(t, conversation, "after restore")
	envelopes := make([]provider.NativeCommitEnvelope, 0)
	for _, entry := range conversation.history.Entries() {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	records := testRestoreRecords(t, envelopes)
	restoredValue, _, err := instance.RestoreConversation(context.Background(), conversation.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	liveFull, err := conversation.compileSamplingRequest(conversation.history.Entries(), restoredInput)
	if err != nil {
		t.Fatal(err)
	}
	restoredFull, err := restored.compileSamplingRequest(restored.history.Entries(), restoredInput)
	if err != nil || !bytes.Equal(liveFull.Bytes(), restoredFull.Bytes()) {
		t.Fatal("restored full logical canonical bytes differ", err)
	}
	liveEvidence, err := conversation.requestEvidence(restoredInput)
	if err != nil {
		t.Fatal(err)
	}
	restoredEvidence, err := restored.requestEvidence(restoredInput)
	if err != nil || liveEvidence.NativeFingerprint() != restoredEvidence.NativeFingerprint() {
		t.Fatal("restored native fingerprint differs", err)
	}
	if websocketCacheSources(t, conversation) != websocketCacheSources(t, restored) {
		t.Fatal("restored content fingerprints differ")
	}
	stream, err = restored.Stream(context.Background(), restoredInput)
	if err != nil {
		t.Fatal(err)
	}
	resumed := readOpenAITerminal(t, restored, stream)
	if resumed.Kind() != provider.StreamEventCompleted {
		t.Fatal(resumed.Error())
	}
	resumedWire := <-requests
	expectedWire, err := websocketBody(liveFull, "", 0)
	if err != nil || !bytes.Equal(resumedWire, expectedWire.Bytes()) {
		t.Fatal("restored connection did not send the full canonical envelope", err)
	}
	receipt, err = resumed.PreparedSample().Receipt()
	if err != nil || receipt.Mode != domain.RequestFull || receipt.Reason != domain.ResetRestored || receipt.RequestFingerprint != domain.ContentFingerprint(liveFull.Bytes()) {
		t.Fatal(receipt, err)
	}
	_ = resumed.PreparedSample().Finalize()
}
func websocketCacheSources(t *testing.T, c *Conversation) contextplan.CacheSources {
	t.Helper()
	profile, _ := contextplan.NewProviderProfile(domain.ProviderOpenAI, c.provider.config.Model)
	profile, err := profile.WithInferenceSettings(c.provider.config.Inference)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := domain.NewEmptyProjectInstructionsSnapshot(32 << 10)
	view, err := c.ProjectActiveContext()
	if err != nil {
		t.Fatal(err)
	}
	_, footprint, err := c.ProjectContextWindow()
	if err != nil {
		t.Fatal(err)
	}
	input, err := contextplan.NewPlanningInput(profile, testOpenAIToolCatalog(t), project, view, footprint, contextplan.NewSamplingInput(), contextplan.DisabledBudget())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := contextplan.NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := contextplan.NewCacheSources(plan)
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

func TestWebSocketPostCreateErrorDoesNotRetry(t *testing.T) {
	var creates, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(500)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, body, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var request websocketCreate
		if codec.Unmarshal(body, &request) != nil {
			return
		}
		if request.Reasoning == nil || request.Reasoning.Effort != "high" || request.Reasoning.Summary != "auto" || request.MaxOutputTokens != 16384 {
			t.Error("create lost frozen settings")
		}
		if request.PreviousResponseID != "" {
			t.Error("failed connection reused")
		}
		n := creates.Add(1)
		if n == 1 {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","error":{"code":"previous_response_not_found","message":"private-body"}}`))
			return
		}
		for _, event := range []string{`{"type":"response.created","response":{"id":"r2"}}`, `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`, `{"type":"response.completed","response":{"id":"r2"}}`} {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(event))
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheOmit, domain.OpenAIWebSocketAuto)
	settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
	instance, err := New(Config{Inference: settings, BaseURL: server.URL, APIKey: secret.New("private-key"), Model: "m", CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	stream, err := c.Stream(context.Background(), testOpenAITurnInput(t, c, "first"))
	if err != nil {
		t.Fatal(err)
	}
	failed := readOpenAITerminal(t, c, stream)
	if failed.Kind() != provider.StreamEventFailed || creates.Load() != 1 || posts.Load() != 0 {
		t.Fatal("generation retried")
	}
	if bytes.Contains([]byte(failed.Error().Error()), []byte("private")) {
		t.Fatal("provider error leaked")
	}
	stream, err = c.Stream(context.Background(), testOpenAITurnInput(t, c, "next explicit input"))
	if err != nil {
		t.Fatal(err)
	}
	completed := readOpenAITerminal(t, c, stream)
	if completed.Kind() != provider.StreamEventCompleted {
		t.Fatal(completed.Error())
	}
	_ = completed.PreparedSample().Finalize()
	if creates.Load() != 2 || posts.Load() != 0 {
		t.Fatal("unexpected request count")
	}
}

func TestWebSocketSummaryUsesSeparateFullConnectionAndResets(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			bodies := make(chan []byte, 4)
			var connects atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				n := connects.Add(1)
				_, body, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				bodies <- body
				if n == 2 && failure {
					_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","error":{"message":"private"}}`))
					return
				}
				id := fmt.Sprintf("r%d", n)
				for _, event := range []string{fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id), `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"goal; tests passed; next task"}]}}`, fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed"}}`, id)} {
					if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
						return
					}
				}
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIWebSocketAuto)
			instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("key"), Model: "model", CachePolicy: policy})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			c := testConversation(t, instance)
			stream, err := c.Stream(context.Background(), testOpenAITurnInput(t, c, "old history"))
			if err != nil {
				t.Fatal(err)
			}
			first := readOpenAITerminal(t, c, stream)
			if first.Kind() != provider.StreamEventCompleted {
				t.Fatal(first.Error())
			}
			_ = first.PreparedSample().Finalize()
			<-bodies
			input, _ := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
			compact, _ := provider.NewCompactionInput(input, "preserve tests", 4096)
			compact, err = compact.WithInstructions("preserve tests\n\nretain hook emphasis")
			if err != nil {
				t.Fatal(err)
			}
			preview, _, err := c.PreflightCompaction(compact)
			if err != nil {
				t.Fatal(err)
			}
			_, before, _ := c.ProjectContextWindow()
			prepared, err := c.RunCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", preview)
			if (err != nil) != failure {
				t.Fatalf("summary result: %v", err)
			}
			var summary websocketCreate
			body := <-bodies
			if codec.Unmarshal(body, &summary) != nil || summary.PreviousResponseID != "" || summary.ToolChoice != "none" || !bytes.Contains(body, []byte(`preserve tests\n\nretain hook emphasis`)) {
				t.Fatal("summary was not a separate full create")
			}
			if failure {
				_, after, _ := c.ProjectContextWindow()
				if before != after {
					t.Fatal("failed summary changed window")
				}
			} else {
				receipt, err := prepared.Receipt()
				if err != nil || receipt.Transport != domain.TransportWebSocket || receipt.Mode != domain.RequestFull || receipt.Purpose != domain.RequestSummary {
					t.Fatal(receipt, err)
				}
				if err := prepared.Finalize(); err != nil {
					t.Fatal(err)
				}
			}
			stream, err = c.Stream(context.Background(), testOpenAITurnInput(t, c, "continue"))
			if err != nil {
				t.Fatal(err)
			}
			next := readOpenAITerminal(t, c, stream)
			if next.Kind() != provider.StreamEventCompleted {
				t.Fatal(next.Error())
			}
			var normal websocketCreate
			if codec.Unmarshal(<-bodies, &normal) != nil || normal.PreviousResponseID != "" || normal.ToolChoice != "" || connects.Load() != 3 {
				t.Fatal("summary left a reusable continuation connection")
			}
			_ = next.PreparedSample().Finalize()
		})
	}
}
