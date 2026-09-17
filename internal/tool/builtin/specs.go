// Package builtin 定义首批内置工具的稳定能力清单。
package builtin

import (
	"encoding/json"

	"easycode/internal/tool"
)

// Specs 返回按稳定顺序排列的内置工具 facade 基线。
func Specs() []tool.Spec {
	objectSchema := json.RawMessage(`{"type":"object","additionalProperties":false}`)
	return []tool.Spec{
		{Name: "Read", Description: "Read a file", Capability: tool.CapabilityReadFile, InputSchema: objectSchema},
		{Name: "Glob", Description: "Find files by pattern", Capability: tool.CapabilitySearch, InputSchema: objectSchema},
		{Name: "Grep", Description: "Search file contents", Capability: tool.CapabilitySearch, InputSchema: objectSchema},
		{Name: "Edit", Description: "Edit a file", Capability: tool.CapabilityPatchFile, InputSchema: objectSchema},
		{Name: "Write", Description: "Write a file", Capability: tool.CapabilityWriteFile, InputSchema: objectSchema},
		{Name: "Bash", Description: "Run a shell command", Capability: tool.CapabilityExec, InputSchema: objectSchema},
	}
}
