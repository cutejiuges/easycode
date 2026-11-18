//go:build darwin

package app

import (
	"encoding/json"
	"io"

	"easycode/internal/domain"
)

func writeExecCommandSample(writer io.Writer, family domain.ProviderFamily, command string, timeout int) {
	encoded, _ := json.Marshal(struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout_ms"`
	}{command, timeout})
	arguments := string(encoded)
	if family == domain.ProviderOpenAI {
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-exec\"}}\n\n")
		writeOpenAIFunctionCall(writer, 0, "fc-exec", "call-exec", "exec_command", arguments)
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-exec\"}}\n\n")
	} else {
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-exec\",\"model\":\"claude-test\"}}\n\n")
		writeAnthropicToolUse(writer, 0, "call-exec", "Bash", arguments)
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}
}
