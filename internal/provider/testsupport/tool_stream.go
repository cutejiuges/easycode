package testsupport

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

// ToolStreamConversation 对照真实原生提交数，拒绝只检查终态却漏掉部分提交。
type ToolStreamConversation interface {
	provider.Conversation
	TestNativeCount() int
}

// CheckToolSSE 保留两家实际 wire 拒绝和取消；Task 的严格 JSON 在所属 decoder 验证。
func CheckToolSSE(t *testing.T, family domain.ProviderFamily, capability tool.CapabilityID, check func(tool.ReadyCall) bool, frames func(*testing.T, string) []string, create func(*testing.T, string) (ToolStreamConversation, provider.TurnInput)) {
	t.Helper()
	type streamCase struct {
		mode  string
		chunk int
	}
	cases := []streamCase{{"valid", 1}, {"partial", 1}}
	if capability == tool.CapabilityExec {
		cases = append(cases, []streamCase{{"valid", 17}, {"duplicate", 1}, {"early-terminal", 1}, {"cancelled", 1}}...)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d", tc.mode, tc.chunk), func(t *testing.T) {
			mode, chunkLimit := tc.mode, tc.chunk
			events := frames(t, mode)
			var body strings.Builder
			for _, event := range events {
				body.WriteString("data: ")
				body.WriteString(event)
				body.WriteString("\n\n")
			}
			started, cleaned := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(cleaned)
				if request.URL.Path != "/prefix/"+family.Wire() {
					t.Error("API prefix lost")
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				data := []byte(body.String())
				random := rand.New(rand.NewSource(19))
				for len(data) > 0 {
					size := min(len(data), 1+random.Intn(chunkLimit))
					if _, err := writer.Write(data[:size]); err != nil {
						return
					}
					writer.(http.Flusher).Flush()
					data = data[size:]
				}
				if mode == "cancelled" {
					close(started)
					<-request.Context().Done()
				}
			}))
			t.Cleanup(server.Close)
			conversation, turn := create(t, server.URL+"/prefix")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := conversation.Stream(ctx, turn)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancelled" {
				<-started
				cancel()
			}
			terminals := 0
			for event := range stream {
				if !event.Kind().Terminal() {
					continue
				}
				terminals++
				if mode != "valid" {
					if event.Kind() == provider.StreamEventCompleted || event.PreparedSample() != nil {
						t.Fatal("invalid tool sample committed")
					}
					continue
				}
				if event.Kind() != provider.StreamEventCompleted || event.PreparedSample() == nil {
					t.Fatalf("valid tool stream failed: %v", event.Error())
				}
				ready, err := event.PreparedSample().ReadyCalls()
				if err != nil || len(ready) != 1 || ready[0].Capability() != capability || !check(ready[0]) {
					t.Fatal("chunked UTF-8 tool changed")
				}
				if err := event.PreparedSample().Finalize(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancelled" {
				<-cleaned
			}
			if terminals != 1 {
				t.Fatalf("terminal count=%d", terminals)
			}
			want := 1
			if mode == "valid" {
				want = 2
			}
			if conversation.TestNativeCount() != want {
				t.Fatal("invalid tool stream changed native history")
			}
		})
	}
}
