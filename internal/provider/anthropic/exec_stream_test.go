package anthropic

import (
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestExecSSEChunkingAndRejectedSamples(t *testing.T) {
	testToolSSESampleModes(t, "Bash", `{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`, []string{`{"command":"printf '`, `你好`, `'","workdir":"src","timeout_ms":1000}`}, tool.CapabilityExec, func(call tool.ReadyCall) bool { return call.ExecInput().Command() == "printf '你好'" })
}

func TestTaskSSEChunkingAndRejectedSamples(t *testing.T) {
	testToolSSESampleModes(t, "TodoWrite", taskNativeInput, []string{taskNativeInput[:12], taskNativeInput[12:]}, tool.CapabilityTaskProgress, func(call tool.ReadyCall) bool {
		return call.TaskInput().Update().Items()[0].Content() == "检查中文代码"
	})
}

func testToolSSESampleModes(t *testing.T, name, input string, parts []string, capability tool.CapabilityID, check func(tool.ReadyCall) bool) {
	t.Helper()
	testsupport.CheckToolSSE(t, domain.ProviderAnthropic, capability, check, func(t *testing.T, mode string) []string {
		count := 1
		if mode == "duplicate" {
			count = 2
		}
		events := []string{`{"type":"message_start","message":{"id":"msg-exec","model":"fixture-model"}}`}
		for index := 0; index < count; index++ {
			events = append(events, fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"call-exec","name":%q,"input":{}}}`, index, name))
			for _, part := range parts {
				events = append(events, fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, index, quoteExecJSON(t, part)))
			}
			if mode != "partial" && mode != "cancelled" {
				events = append(events, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
			}
		}
		if mode != "partial" && mode != "cancelled" {
			events = append(events, `{"type":"message_stop"}`)
		}

		if mode == "early-terminal" {
			events = append(events[:2], events[len(events)-1])
		}
		return events
	}, func(t *testing.T, url string) (testsupport.ToolStreamConversation, provider.TurnInput) {
		instance, err := New(Config{BaseURL: url, APIKey: secret.New("fixture-key"), Model: "fixture-model"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := instance.Close(); err != nil {
				t.Error(err)
			}
		})
		conversation := testConversation(t, instance)
		testCommitUserInput(t, conversation, "test")
		turn, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		return conversation, turn
	})
}
func (conversation *Conversation) TestNativeCount() int { return len(conversation.history.Entries()) }
