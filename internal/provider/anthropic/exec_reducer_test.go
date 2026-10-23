package anthropic

import (
	"easycode/internal/tool"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func quoteExecJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
func TestExecReducerStrictInputs(t *testing.T) {
	for _, input := range []string{
		`{"command":"printf hello","workdir":"src","timeout_ms":1000}`,
		`{"command":""}`, `{"command":"true","unknown":true}`,
		`{"command":"true","command":"false"}`, `{"command":"true"}{}`,
		`{"command":"true","workdir":"../outside"}`, `{"command":"true","timeout_ms":0}`,
		`{"command":"unfinished"`,
		`{"command":"` + strings.Repeat("x", tool.MaxExecCommandBytes+1) + `"}`,
	} {
		t.Run(input[:min(len(input), 60)], func(t *testing.T) {
			reducer := newStreamReducer(testAnthropicToolCatalog(t))
			events := []string{`{"type":"message_start","message":{"id":"msg-exec","model":"claude-test"}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-exec","name":"Bash","input":{}}}`}
			for _, chunk := range []string{input[:len(input)/2], input[len(input)/2:]} {
				events = append(events, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%s}}`, quoteExecJSON(t, chunk)))
			}
			events = append(events, `{"type":"content_block_stop","index":0}`, `{"type":"message_stop"}`)
			var err error
			for index, event := range events {
				if index < 4 && len(reducer.readyCalls()) != 0 {
					t.Fatal("partial Exec input produced ready call")
				}
				_, err = reduceAnthropicTestEvent(reducer, event)
				if err != nil {
					break
				}
			}
			valid := input == `{"command":"printf hello","workdir":"src","timeout_ms":1000}`
			ready := reducer.readyCalls()
			if !valid {
				if err == nil || len(ready) != 0 {
					t.Fatal("invalid Exec input produced ready call")
				}
				return
			}
			if err != nil || len(ready) != 1 || ready[0].Capability() != tool.CapabilityExec || ready[0].ProviderCallID() != "call-exec" || ready[0].ExecInput().Command() != "printf hello" || ready[0].ExecInput().Workdir() != "src" || ready[0].ExecInput().TimeoutMillis() != 1000 {
				t.Fatalf("Exec input lost: %v", err)
			}
		})
	}
}
