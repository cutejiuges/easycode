package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"github.com/coder/websocket"
)

func TestRuntimeEffortWebsocketPropertiesNoopFailureAndSummaryReset(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel%t", cancelled), func(t *testing.T) { runEffortWebsocketReset(t, cancelled) })
	}
}
func runEffortWebsocketReset(t *testing.T, cancelled bool) {
	cancelEntered := make(chan struct{})
	requests := make(chan []byte, 16)
	var serial atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			requests <- body
			n := serial.Add(1)
			var create websocketCreate
			if codec.Unmarshal(body, &create) != nil {
				return
			}
			if n == 5 && cancelled {
				close(cancelEntered)
				_, _, _ = conn.Read(r.Context())
				return
			}
			if n == 5 || create.ToolChoice == "none" {
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","code":"fixture_endpoint_failure","message":"private fixture"}`))
				return
			}
			id := fmt.Sprintf("response-%d", n)
			for _, event := range []string{fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id), `{"type":"response.output_item.done","item":{"type":"message","id":"msg","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}`, fmt.Sprintf(`{"type":"response.completed","response":{"id":%q}}`, id)} {
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIWebSocketAuto)
	startup, _ := domain.NewOpenAIInference("high", "auto", 16384)
	instance, err := New(Config{Inference: startup, CachePolicy: policy, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	for i, level := range []domain.EffortLevel{"high", "high", "low", "low", "low", "high", "high"} {
		// 第二次 high 代表没有模型操作介入的 high→low→high；过程选择不进入 Kernel。
		choice, _ := domain.NewExplicitEffortChoice(level)
		settings, err := choice.Apply(startup)
		if err != nil {
			t.Fatal(err)
		}
		testCommitUserInput(t, c, fmt.Sprintf("task-%d", i))
		input, _ := testSamplingInput(t, settings).WithToolCatalog(testOpenAIToolCatalog(t))
		operationCtx, cancelOperation := context.WithCancel(context.Background())
		stream, err := c.Stream(operationCtx, input)
		if err != nil {
			t.Fatal(err)
		}
		if i == 4 && cancelled {
			<-cancelEntered
			cancelOperation()
		}
		terminal := readOpenAITerminal(t, c, stream)
		cancelOperation()
		raw := <-requests
		var wire websocketCreate
		if codec.Unmarshal(raw, &wire) != nil || wire.Reasoning == nil || domain.EffortLevel(wire.Reasoning.Effort) != level || wire.Store {
			t.Fatal("wrong selected wire", string(raw))
		}
		if i == 4 {
			if terminal.Kind() == provider.StreamEventCompleted {
				t.Fatal("fixture error completed")
			}
			continue
		}
		if terminal.Kind() != provider.StreamEventCompleted {
			t.Fatal("operation failed", terminal.Error())
		}
		receipt, err := terminal.PreparedSample().Receipt()
		if err != nil {
			t.Fatal(err)
		}
		incremental := i == 1 || i == 3 || i == 6
		if (wire.PreviousResponseID != "") != incremental || (receipt.Mode == domain.RequestIncremental) != incremental {
			t.Fatal("property/reset baseline incorrect", i, receipt, string(raw))
		}
		if i == 2 && receipt.Reason != domain.ResetProperties {
			t.Fatal("effort change did not identify properties source", receipt)
		}
		if i == 5 && wire.PreviousResponseID != "" {
			t.Fatal("failed intermediate low restored old response id")
		}
		if err := terminal.PreparedSample().Finalize(); err != nil {
			t.Fatal(err)
		}
	}
	sampling, _ := testSamplingInput(t, startup).WithToolCatalog(testOpenAIToolCatalog(t))
	compact, _ := provider.NewCompactionInput(sampling, "preserve failures", 4096)
	preview, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	if prepared, err := c.RunCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", preview); err == nil || prepared != nil {
		t.Fatal("summary fixture did not fail")
	}
	var summary websocketCreate
	raw := <-requests
	if codec.Unmarshal(raw, &summary) != nil || summary.PreviousResponseID != "" || summary.ToolChoice != "none" || summary.MaxOutputTokens != 4096 {
		t.Fatal("summary reused ordinary baseline")
	}
	testCommitUserInput(t, c, "after summary failure")
	stream, err := c.Stream(context.Background(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	terminal := readOpenAITerminal(t, c, stream)
	raw = <-requests
	var final websocketCreate
	_ = codec.Unmarshal(raw, &final)
	if terminal.Kind() != provider.StreamEventCompleted || final.PreviousResponseID != "" || !bytes.Contains(raw, []byte(`"effort":"high"`)) {
		t.Fatal("summary failure retained invalid baseline", string(raw))
	}
	if err := terminal.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
}
