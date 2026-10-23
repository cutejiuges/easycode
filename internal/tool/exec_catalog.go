package tool

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"errors"
)

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

// NewExecCatalogSnapshot 注册具有完整消费者的前台命令能力。
func NewExecCatalogSnapshot(read ReadExecutor, glob GlobExecutor, grep GrepExecutor, patchPreparer PatchPreparer, patch PatchExecutor, execPreparer ExecPreparer, executor ExecExecutor, renderer ExecOutputRenderer, policy Policy) (CatalogSnapshot, error) {
	if read == nil || glob == nil || grep == nil || patchPreparer == nil || patch == nil || execPreparer == nil || executor == nil || renderer == nil || policy == nil {
		return CatalogSnapshot{}, errors.New("all tool components are required")
	}
	facades, err := buildFileFacades()
	if err != nil {
		return CatalogSnapshot{}, err
	}
	schema, err := codec.MarshalCanonical(execInputSchema{Type: "object", Properties: execInputSchemaProperties{Command: stringSchemaProperty{Type: "string", Description: "Foreground non-interactive shell command, at most 64 KiB UTF-8. Detached processes are unsupported."}, Workdir: stringSchemaProperty{Type: "string", Description: "Optional workspace-relative working directory"}, TimeoutMillis: integerSchemaProperty{Type: "integer", Description: "Timeout in milliseconds; defaults to 120000", Minimum: MinExecTimeoutMillis, Maximum: MaxExecTimeoutMillis}}, Required: []string{"command"}, AdditionalProperties: false}, maxCatalogBytes)
	if err != nil {
		return CatalogSnapshot{}, err
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		name, id := facadeIdentity(family, CapabilityExec)
		facades = append(facades, Facade{id: id, family: family, name: name, description: "Runs a foreground command after one-time approval, with workspace writes, no network, bounded output and timeout.", capability: CapabilityExec, wireKind: WireStructured, schema: schema.Clone()})
	}
	return newCatalogSnapshot(facades)
}
