package openai

import (
	"easycode/internal/provider/transport"
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
			reducer := newResponsesStreamReducer(testOpenAIToolCatalog(t))
			events := []string{`{"type":"response.created","response":{"id":"resp-exec"}}`, `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc-exec","call_id":"call-exec","name":"exec_command"}}`}
			for _, chunk := range []string{input[:len(input)/2], input[len(input)/2:]} {
				events = append(events, fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":"fc-exec","output_index":0,"delta":%s}`, quoteExecJSON(t, chunk)))
			}
			events = append(events, fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":"fc-exec","output_index":0,"arguments":%s}`, quoteExecJSON(t, input)), fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc-exec","call_id":"call-exec","name":"exec_command","arguments":%s}}`, quoteExecJSON(t, input)), `{"type":"response.completed","response":{"id":"resp-exec"}}`)
			var err error
			for index, event := range events {
				if index < 4 && len(reducer.readyCalls()) != 0 {
					t.Fatal("partial Exec input produced ready call")
				}
				_, err = reducer.reduce(transport.SSEEvent{Data: event})
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
