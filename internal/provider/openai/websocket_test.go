package openai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
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
	runWebSocketDurableIncrementalAndReset(t, false)
}
func TestReasoningWebSocketDurableIncrementalAndReset(t *testing.T) {
	runWebSocketDurableIncrementalAndReset(t, true)
}
func runWebSocketDurableIncrementalAndReset(t *testing.T, active bool) {
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
	settings, _ := domain.DefaultInferenceSettings(domain.ProviderOpenAI)
	if active {
		settings, _ = domain.NewOpenAIInference("high", "auto", 16384)
	}
	instance, err := New(Config{Inference: settings, BaseURL: server.URL + "/v1/", APIKey: secret.New("ws-test"), Model: "gpt-test", CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	firstInput := testOpenAITurnInput(t, conversation, "first")
	firstFull, _ := conversation.compileSamplingRequest(conversation.history.snapshot(), firstInput)
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
	goldenPath := "testdata/websocket_full.golden.json"
	if active {
		goldenPath = "testdata/reasoning_websocket_full.golden.json"
	}
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
	full, err := conversation.compileSamplingRequest(conversation.history.snapshot(), secondInput)
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
	goldenPath = "testdata/websocket_incremental.golden.json"
	if active {
		goldenPath = "testdata/reasoning_websocket_incremental.golden.json"
	}
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
	for _, entry := range conversation.history.snapshot() {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	records := testRestoreRecords(t, envelopes)
	restoredValue, err := instance.RestoreConversation(conversation.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	liveFull, err := conversation.compileSamplingRequest(conversation.history.snapshot(), restoredInput)
	if err != nil {
		t.Fatal(err)
	}
	restoredFull, err := restored.compileSamplingRequest(restored.history.snapshot(), restoredInput)
	if err != nil || !bytes.Equal(liveFull.Bytes(), restoredFull.Bytes()) {
		t.Fatal("restored full logical canonical bytes differ", err)
	}
	liveEvidence, err := conversation.requestEvidence(restoredInput)
	if err != nil {
		t.Fatal(err)
	}
	restoredEvidence, err := restored.requestEvidence(restoredInput)
	if err != nil || liveEvidence.nativeFingerprint != restoredEvidence.nativeFingerprint {
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
	footprint, err := c.HistoryFootprint()
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

func TestWebSocketHandshakeFallbackIsSticky(t *testing.T) {
	for _, status := range []int{404, 405, 426, 400, 401, 403, 429, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var handshakes, posts atomic.Int32
			hashes := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					handshakes.Add(1)
					w.WriteHeader(status)
					return
				}
				posts.Add(1)
				body, _ := io.ReadAll(r.Body)
				var logical responsesRequest
				if codec.Unmarshal(body, &logical) != nil || logical.Reasoning == nil || logical.Reasoning.Effort != "high" || logical.Reasoning.Summary != "auto" || logical.MaxOutputTokens != 16384 {
					t.Error("fallback lost frozen settings")
				}
				hashes <- domain.ContentFingerprint(body)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\"}}\n\n")
			}))
			defer server.Close()
			policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheOmit, domain.OpenAIWebSocketAuto)
			settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
			instance, err := New(Config{Inference: settings, BaseURL: server.URL, APIKey: secret.New("key"), Model: "m", CachePolicy: policy})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			c := testConversation(t, instance)
			fallback := status == 404 || status == 405 || status == 426
			for n := 0; n < 2; n++ {
				input := testOpenAITurnInput(t, c, fmt.Sprint(n))
				stream, err := c.Stream(context.Background(), input)
				if !fallback {
					if err == nil {
						for range stream {
						}
						t.Fatal("unsafe fallback")
					}
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				terminal := readOpenAITerminal(t, c, stream)
				if terminal.Kind() != provider.StreamEventCompleted {
					t.Fatal(terminal.Error())
				}
				receipt, _ := terminal.PreparedSample().Receipt()
				if receipt.RequestFingerprint != <-hashes || receipt.Transport != domain.TransportHTTP || receipt.Fallback != domain.FallbackHandshake {
					t.Fatal(receipt)
				}
				_ = terminal.PreparedSample().Finalize()
			}
			if handshakes.Load() != 1 || (fallback && posts.Load() != 2) || (!fallback && posts.Load() != 0) {
				t.Fatalf("handshakes=%d posts=%d", handshakes.Load(), posts.Load())
			}
		})
	}
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

func TestWebSocketEnvelopeCoversEveryLogicalProperty(t *testing.T) {
	logical := reflect.TypeOf(responsesRequest{})
	envelope := reflect.TypeOf(websocketCreate{})
	fields := make(map[string]bool)
	for i := 0; i < envelope.NumField(); i++ {
		fields[envelope.Field(i).Name] = true
	}
	for i := 0; i < logical.NumField(); i++ {
		name := logical.Field(i).Name
		if name != "Stream" && !fields[name] {
			t.Fatalf("logical property %s is missing from websocket envelope", name)
		}
	}
	if fields["Stream"] {
		t.Fatal("HTTP stream option entered websocket create")
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
			input, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
			compact, _ := provider.NewCompactionInput(input, "preserve tests", 4096)
			preview, err := c.PreflightCompaction(compact)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := c.HistoryFootprint()
			prepared, err := c.RunCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", preview)
			if (err != nil) != failure {
				t.Fatalf("summary result: %v", err)
			}
			var summary websocketCreate
			if codec.Unmarshal(<-bodies, &summary) != nil || summary.PreviousResponseID != "" || summary.ToolChoice != "none" {
				t.Fatal("summary was not a separate full create")
			}
			if failure {
				after, _ := c.HistoryFootprint()
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

func TestEveryLogicalPropertyChangeInvalidatesContinuation(t *testing.T) {
	original := responsesRequest{Model: "model", PromptCacheKey: "key", Tools: []responseTool{{Type: "function", Name: "Read", Description: "stable"}}, Stream: true, Include: []string{"reasoning.encrypted_content"}}
	properties, err := codec.MarshalCanonical(original, maxResponsesRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	item, err := codec.MarshalCanonical(NewUserItem("native input"), maxResponsesRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := provider.NewRequestBaseline([]codec.CanonicalJSON{item}, nil, properties)
	if err != nil {
		t.Fatal(err)
	}
	changes := []struct {
		name  string
		apply func(*responsesRequest)
	}{
		{"Reasoning", func(r *responsesRequest) { r.Reasoning = &reasoningWire{Effort: "high", Summary: "auto"} }},
		{"Model", func(r *responsesRequest) { r.Model = "other" }}, {"PromptCacheKey", func(r *responsesRequest) { r.PromptCacheKey = "other" }},
		{"ToolChoice", func(r *responsesRequest) { r.ToolChoice = "none" }}, {"MaxOutputTokens", func(r *responsesRequest) { r.MaxOutputTokens = 1 }},
		{"Tools", func(r *responsesRequest) {
			r.Tools = []responseTool{{Type: "function", Name: "Read", Description: "changed"}}
		}},
		{"Stream", func(r *responsesRequest) { r.Stream = false }}, {"Store", func(r *responsesRequest) { r.Store = true }}, {"Include", func(r *responsesRequest) { r.Include = []string{"other"} }},
	}
	if len(changes) != reflect.TypeOf(original).NumField()-1 {
		t.Fatal("new logical properties require invalidation coverage")
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			request := original
			change.apply(&request)
			props, err := codec.MarshalCanonical(request, maxResponsesRequestBytes)
			if err != nil {
				t.Fatal(err)
			}
			offset, relation, reason := baseline.Compare([]codec.CanonicalJSON{item}, props)
			if offset != 0 || relation != domain.NativeReplaced || reason != domain.ResetProperties {
				t.Fatal(offset, relation, reason)
			}
		})
	}
}
