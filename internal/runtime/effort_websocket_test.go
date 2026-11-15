package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
	"github.com/coder/websocket"
)

// 使用真实 owner 与连接验证：没有执行的中间选择不破坏基线，已接纳但网络前取消的摘要必须破坏基线。
func TestEffortOwnerWebSocketNoopAndPreNetworkSummaryCancellation(t *testing.T) {
	bodies := make(chan []byte, 8)
	closed := make(chan struct{}, 8)
	var connections, sequence atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		connections.Add(1)
		defer conn.CloseNow()
		defer func() { closed <- struct{}{} }()
		for {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			bodies <- body
			id := fmt.Sprintf("effort-response-%d", sequence.Add(1))
			for _, event := range []string{fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id), `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`, fmt.Sprintf(`{"type":"response.completed","response":{"id":%q}}`, id)} {
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIWebSocketAuto)
	startup, _ := domain.NewOpenAIInference("high", "auto", 16384)
	p, err := openai.New(openai.Config{BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "gpt-test", CachePolicy: policy, Inference: startup})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c, err := p.NewConversation(runtimeThreadID)
	if err != nil {
		t.Fatal(err)
	}
	j := &effortPreNetworkCompactionJournal{fakeJournal: &fakeJournal{}, entered: make(chan struct{})}
	cfg := testRuntimeConfig(t, j)
	cfg.ContextProfile, err = cfg.ContextProfile.WithInferenceSettings(startup)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CachePolicy, cfg.GenerateTurnID = policy, sequentialTurnIDs()
	runtime, err := New(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	loop, err := NewAgentLoop(runtime, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := startAgentLoop(t, loop)
	t.Cleanup(func() { _ = loop.Stop(context.Background()) })
	serial := 0
	idle := func() {
		for {
			serial++
			if queryEffortLoop(t, loop, protocol.RequestID(fmt.Sprintf("idle-%d", serial))).Activity == "idle" {
				return
			}
		}
	}
	queryCache := func(id protocol.RequestID) protocol.CacheSnapshotPayload {
		cmd, _ := protocol.NewQueryCacheCommand(id)
		if result, err := loop.Submit(context.Background(), cmd); err != nil || result.Status() != protocol.CommandStatusAccepted {
			t.Fatal("cache query failed", err)
		}
		for item := range loop.Outputs() {
			if event, ok := item.ContextEvent(); ok && item.RequestID() == id && event.Kind == protocol.EventCacheSnapshot {
				payload, err := protocol.DecodeCacheSnapshot(event)
				if err != nil {
					t.Fatal(err)
				}
				return payload
			}
		}
		t.Fatal("missing cache snapshot")
		return protocol.CacheSnapshotPayload{}
	}
	turn := func(id protocol.RequestID, incremental bool, inputItems int) protocol.CacheSnapshotPayload {
		submitLoopCommand(t, loop, id, string(id))
		receiveTurnEvent(t, loop.Outputs(), id, protocol.EventTurnCompleted)
		idle()
		body := <-bodies
		var wire struct {
			Previous  string            `json:"previous_response_id"`
			Input     []json.RawMessage `json:"input"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := codec.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		cache := queryCache(protocol.RequestID("cache-" + id))
		if wire.Reasoning.Effort != "high" || (wire.Previous != "") != incremental || len(wire.Input) != inputItems || cache.LastSample == nil || (cache.LastSample.Mode == domain.RequestIncremental) != incremental {
			t.Fatalf("wire/receipt mismatch: %s, %#v", body, cache.LastSample)
		}
		if !incremental && id == "after-summary-cancel" && (!bytes.Contains(body, []byte("first")) || !bytes.Contains(body, []byte("unexecuted-choice"))) {
			t.Fatal("full restart lost native history", string(body))
		}
		return cache
	}
	first := turn("first", false, 1)
	setEffortLoop(t, loop, "same-wire-source", effortChoice(t, "high"))
	setEffortLoop(t, loop, "unexecuted-low", effortChoice(t, "low"))
	setEffortLoop(t, loop, "back-high", effortChoice(t, "high"))
	before := queryCache("before-second")
	if before.LastSample.RequestFingerprint != first.LastSample.RequestFingerprint || before.StableFingerprint != first.StableFingerprint {
		t.Fatal("unexecuted settings replaced committed cache evidence")
	}
	turn("unexecuted-choice", true, 1)
	if connections.Load() != 1 {
		t.Fatal("same wire or unexecuted selection reconnected")
	}
	beforeRecords := len(j.snapshot())
	setEffortLoop(t, loop, "summary-low", effortChoice(t, "low"))
	compact, _ := protocol.NewCompactContextCommand("summary", "")
	result, err := loop.Submit(context.Background(), compact)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("manual summary rejected", result, err)
	}
	<-j.entered
	setEffortLoop(t, loop, "following-high", effortChoice(t, "high"))
	cancel, _ := protocol.NewCancelCompactionCommand("cancel-summary", result.CompactionID())
	if result, err := loop.Submit(context.Background(), cancel); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("manual cancellation rejected", err)
	}
	idle()
	if runtime.operationProfile.InferenceSettings() != startup {
		t.Fatal("joined summary retained a transient operation profile")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled summary did not close old continuation before idle")
	}
	if len(j.snapshot()) != beforeRecords || sequence.Load() != 2 {
		t.Fatal("cancelled initial write committed facts or performed summary I/O")
	}
	turn("after-summary-cancel", false, 5)
	if connections.Load() != 2 {
		t.Fatal("cancelled intermediate operation retained its old socket")
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	go drainControlItems(loop.Outputs())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
