package tool

import "easycode/internal/codec"

type execInputSchema struct {
	Type                 string                    `json:"type"`
	Properties           execInputSchemaProperties `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}
type execInputSchemaProperties struct {
	Command       stringSchemaProperty  `json:"command"`
	Workdir       stringSchemaProperty  `json:"workdir"`
	TimeoutMillis integerSchemaProperty `json:"timeout_ms"`
}

// buildExecSchema 构造前台命令能力的确定性输入 schema。
func buildExecSchema() (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(execInputSchema{Type: "object", Properties: execInputSchemaProperties{Command: stringSchemaProperty{Type: "string", Description: "Foreground non-interactive shell command, at most 64 KiB UTF-8. Detached processes are unsupported."}, Workdir: stringSchemaProperty{Type: "string", Description: "Optional workspace-relative working directory"}, TimeoutMillis: integerSchemaProperty{Type: "integer", Description: "Timeout in milliseconds; defaults to 120000", Minimum: MinExecTimeoutMillis, Maximum: MaxExecTimeoutMillis}}, Required: []string{"command"}, AdditionalProperties: false}, maxCatalogBytes)
}
