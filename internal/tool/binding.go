package tool

import (
	"errors"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// Components 是当前十项工具能力使用的执行器、准备器及审批策略。
type Components struct {
	Read         ReadExecutor
	Glob         GlobExecutor
	Grep         GrepExecutor
	PreparePatch PatchPreparer
	Patch        PatchExecutor
	PrepareWrite WritePreparer
	Write        WriteExecutor
	PrepareExec  ExecPreparer
	Exec         ExecExecutor
	Policy       Policy
	ReadArtifact ReadArtifactExecutor
	Skill        SkillExecutor
	Question     QuestionExecutor
	Task         TaskExecutor
}

// Binding 从同一组显式组件提供可见目录和运行所需的执行依赖。
type Binding struct {
	catalog    CatalogSnapshot
	components Components
}

// NewBinding 检查组件完整性并在内存中生成对应的确定性目录。
func NewBinding(components Components) (Binding, error) {
	registrations := [...]capabilityRegistration{
		structuredCapability(CapabilityRead, components.Read != nil, buildReadSchema,
			"Reads a UTF-8 text file from the local workspace with stable 1-based line numbers."),
		structuredCapability(CapabilityGlob, components.Glob != nil, buildGlobSchema,
			"Finds regular files in the local workspace using a deterministic workspace-relative glob pattern."),
		structuredCapability(CapabilityGrep, components.Grep != nil, buildGrepSchema,
			"Searches UTF-8 text files in the local workspace using deterministic line-oriented RE2 semantics."),
		{installed: components.PreparePatch != nil && components.Patch != nil, facades: buildPatchFacades},
		structuredCapability(CapabilityWrite, components.PrepareWrite != nil && components.Write != nil, buildWriteSchema,
			"Creates or overwrites one UTF-8 workspace file with exact content after approval. Overwriting requires a complete Read delivered in an earlier sample; truncated or same-sample reads do not qualify. Missing parent directories are created atomically. Use Edit or apply_patch for partial changes."),
		structuredCapability(CapabilityExec, components.PrepareExec != nil && components.Exec != nil, buildExecSchema,
			"Runs a foreground command after one-time approval, with workspace writes, no network, bounded output and timeout."),
		structuredCapability(CapabilityReadArtifact, components.ReadArtifact != nil, buildArtifactSchema,
			"Reads a saved command-output prefix by artifact ID from the current thread. Offsets and limits count raw bytes; partial storage EOF does not mean complete command output."),
		structuredCapability(CapabilitySkill, components.Skill != nil, buildSkillSchema,
			"Loads complete local skill instructions by name, or reads a relative supporting resource from the same source. Does not grant tool permissions. args and resource are mutually exclusive."),
		structuredCapability(CapabilityQuestion, components.Question != nil, buildQuestionSchema,
			"Asks the user one to four questions with single or multiple choices and optional notes, then waits for explicit answers. The client adds Other. Put a recommended choice first with (Recommended) in its label. Ask before generating tool calls whose parameters depend on the answer. Answers never grant tool permissions. Requires an interactive host."),
		{installed: components.Task != nil, facades: buildTaskFacades},
	}
	if components.Policy == nil {
		return Binding{}, errors.New("all tool components are required")
	}
	facades := make([]Facade, 0, 2*len(registrations))
	for _, registration := range registrations {
		if !registration.installed {
			return Binding{}, errors.New("all tool components are required")
		}
		capabilityFacades, err := registration.facades()
		if err != nil {
			return Binding{}, err
		}
		facades = append(facades, capabilityFacades...)
	}
	for i := range facades {
		facades[i].serverSchema = serverSchemaMode(facades[i].family, facades[i].wireKind, facades[i].capability)
	}
	catalog, err := newCatalogSnapshot(facades, domain.WorkModeDefault)
	if err != nil {
		return Binding{}, err
	}
	return Binding{catalog: catalog, components: components}, nil
}

type capabilityRegistration struct {
	installed bool
	facades   func() ([]Facade, error)
}

func structuredCapability(capability CapabilityID, installed bool, buildSchema func() (codec.CanonicalJSON, error), description string) capabilityRegistration {
	return capabilityRegistration{installed: installed, facades: func() ([]Facade, error) {
		schema, err := buildSchema()
		if err != nil {
			return nil, err
		}
		return structuredFacades(capability, description, schema), nil
	}}
}

// Catalog 返回不持有执行依赖的不可变模型目录。
func (binding Binding) Catalog() CatalogSnapshot { return binding.catalog }

// Components 返回与目录同时绑定的执行依赖。
func (binding Binding) Components() Components { return binding.components }
