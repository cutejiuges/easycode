package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const (
	maxCatalogBytes = 1 << 20

	FacadeAnthropicRead  FacadeID = "anthropic:Read"
	FacadeOpenAIRead     FacadeID = "openai:Read"
	FacadeAnthropicGlob  FacadeID = "anthropic:Glob"
	FacadeOpenAIGlob     FacadeID = "openai:Glob"
	FacadeAnthropicGrep  FacadeID = "anthropic:Grep"
	FacadeOpenAIGrep     FacadeID = "openai:Grep"
	FacadeAnthropicEdit  FacadeID = "anthropic:Edit"
	FacadeOpenAIPatch    FacadeID = "openai:apply_patch"
	FacadeAnthropicWrite FacadeID = "anthropic:Write"
	FacadeOpenAIWrite    FacadeID = "openai:Write"
	FacadeAnthropicBash  FacadeID = "anthropic:Bash"
	FacadeOpenAIExec     FacadeID = "openai:exec_command"
)

// FacadeID 唯一标识某个 Provider family 的模型可见 facade。
type FacadeID string

// Facade 是 Provider 请求编译所需的不可变视图。
type Facade struct {
	id          FacadeID
	family      domain.ProviderFamily
	name        string
	description string
	capability  CapabilityID
	wireKind    ProviderWireKind
	schema      codec.CanonicalJSON
	format      FreeformFormat
}

// FreeformFormat 保存OpenAI custom tool的确定性grammar定义。
type FreeformFormat struct{ kind, syntax, definition string }

func (format FreeformFormat) Type() string       { return format.kind }
func (format FreeformFormat) Syntax() string     { return format.syntax }
func (format FreeformFormat) Definition() string { return format.definition }
func (format FreeformFormat) zero() bool {
	return format.kind == "" && format.syntax == "" && format.definition == ""
}

func (facade Facade) ID() FacadeID                   { return facade.id }
func (facade Facade) Family() domain.ProviderFamily  { return facade.family }
func (facade Facade) Name() string                   { return facade.name }
func (facade Facade) Description() string            { return facade.description }
func (facade Facade) Capability() CapabilityID       { return facade.capability }
func (facade Facade) WireKind() ProviderWireKind     { return facade.wireKind }
func (facade Facade) InputSchema() []byte            { return facade.schema.Bytes() }
func (facade Facade) FreeformFormat() FreeformFormat { return facade.format }

func (facade Facade) clone() Facade {
	facade.schema = facade.schema.Clone()
	return facade
}

func (facade Facade) validate() error {
	if !facade.family.Valid() || facade.name == "" || facade.description == "" || !facade.capability.Valid() || !facade.wireKind.Valid() {
		return errors.New("invalid tool facade")
	}
	wantName, wantID := facadeIdentity(facade.family, facade.capability)
	if facade.name != wantName || facade.id != wantID {
		return errors.New("tool facade identity does not match capability")
	}
	switch facade.wireKind {
	case WireStructured:
		if !facade.format.zero() || facade.schema.Len() == 0 {
			return errors.New("structured tool facade definition is invalid")
		}
		return facade.schema.Validate(maxCatalogBytes)
	case WireCustomFreeform:
		if facade.schema.Len() != 0 || facade.format.kind != "grammar" || facade.format.syntax != "lark" || facade.format.definition == "" || len(facade.format.definition) > maxCatalogBytes {
			return errors.New("custom tool facade definition is invalid")
		}
		return nil
	default:
		return errors.New("tool facade wire kind is invalid")
	}
}

func facadeIdentity(family domain.ProviderFamily, capability CapabilityID) (string, FacadeID) {
	prefix := "openai:"
	if family == domain.ProviderAnthropic {
		prefix = "anthropic:"
	}
	switch capability {
	case CapabilityExec:
		if family == domain.ProviderAnthropic {
			return "Bash", FacadeAnthropicBash
		}
		return "exec_command", FacadeOpenAIExec
	case CapabilityRead:
		return "Read", FacadeID(prefix + "Read")
	case CapabilityGlob:
		return "Glob", FacadeID(prefix + "Glob")
	case CapabilityGrep:
		return "Grep", FacadeID(prefix + "Grep")
	case CapabilityWrite:
		return "Write", FacadeID(prefix + "Write")
	case CapabilityPatch:
		if family == domain.ProviderAnthropic {
			return "Edit", FacadeAnthropicEdit
		}
		return "apply_patch", FacadeOpenAIPatch
	default:
		return "", ""
	}
}

// FacadeView 是单个 Provider family 的稳定 facade 快照。
type FacadeView struct {
	family      domain.ProviderFamily
	facades     []Facade
	canonical   codec.CanonicalJSON
	fingerprint string
}

func (view FacadeView) Family() domain.ProviderFamily { return view.family }
func (view FacadeView) Facades() []Facade             { return cloneFacades(view.facades) }
func (view FacadeView) CanonicalJSON() []byte         { return view.canonical.Bytes() }
func (view FacadeView) Fingerprint() string           { return view.fingerprint }

// CatalogSnapshot 保存 Read、Glob、Grep、Patch、Write、Exec 六项真实能力的确定性目录。
type CatalogSnapshot struct {
	facades     []Facade
	canonical   codec.CanonicalJSON
	fingerprint string
}

type stringSchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type integerSchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Minimum     int    `json:"minimum"`
	Maximum     int    `json:"maximum,omitempty"`
}

type booleanSchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type enumSchemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
}

type readInputSchema struct {
	Type                 string                    `json:"type"`
	Properties           readInputSchemaProperties `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type readInputSchemaProperties struct {
	FilePath stringSchemaProperty  `json:"file_path"`
	Offset   integerSchemaProperty `json:"offset"`
	Limit    integerSchemaProperty `json:"limit"`
}

type globInputSchema struct {
	Type                 string                    `json:"type"`
	Properties           globInputSchemaProperties `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type globInputSchemaProperties struct {
	Pattern stringSchemaProperty  `json:"pattern"`
	Path    stringSchemaProperty  `json:"path"`
	Limit   integerSchemaProperty `json:"limit"`
}

type grepInputSchema struct {
	Type                 string                    `json:"type"`
	Properties           grepInputSchemaProperties `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type grepInputSchemaProperties struct {
	Pattern         stringSchemaProperty  `json:"pattern"`
	Path            stringSchemaProperty  `json:"path"`
	Glob            stringSchemaProperty  `json:"glob"`
	OutputMode      enumSchemaProperty    `json:"output_mode"`
	CaseInsensitive booleanSchemaProperty `json:"case_insensitive"`
	BeforeContext   integerSchemaProperty `json:"before_context"`
	AfterContext    integerSchemaProperty `json:"after_context"`
	Limit           integerSchemaProperty `json:"limit"`
}

type editInputSchema struct {
	Type                 string                    `json:"type"`
	Properties           editInputSchemaProperties `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type editInputSchemaProperties struct {
	FilePath   stringSchemaProperty  `json:"file_path"`
	OldString  stringSchemaProperty  `json:"old_string"`
	NewString  stringSchemaProperty  `json:"new_string"`
	ReplaceAll booleanSchemaProperty `json:"replace_all"`
}

type facadeCanonical struct {
	ID          FacadeID                 `json:"id"`
	Family      domain.ProviderFamily    `json:"family"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Capability  CapabilityID             `json:"capability"`
	WireKind    ProviderWireKind         `json:"wire_kind"`
	InputSchema json.RawMessage          `json:"input_schema,omitempty"`
	Format      *freeformFormatCanonical `json:"format,omitempty"`
}

type freeformFormatCanonical struct {
	Type       string `json:"type"`
	Syntax     string `json:"syntax"`
	Definition string `json:"definition"`
}

type catalogCanonical struct {
	Facades []facadeCanonical `json:"facades"`
}

// buildReadOnlyFacades 只构造真实只读子集，不建立可执行的旧目录契约。
func buildReadOnlyFacades() ([]Facade, error) {
	readSchema, err := buildReadSchema()
	if err != nil {
		return nil, err
	}
	globSchema, err := buildGlobSchema()
	if err != nil {
		return nil, err
	}
	grepSchema, err := buildGrepSchema()
	if err != nil {
		return nil, err
	}
	descriptions := map[CapabilityID]string{
		CapabilityRead: "Reads a UTF-8 text file from the local workspace with stable 1-based line numbers.",
		CapabilityGlob: "Finds regular files in the local workspace using a deterministic workspace-relative glob pattern.",
		CapabilityGrep: "Searches UTF-8 text files in the local workspace using deterministic line-oriented RE2 semantics.",
	}
	schemas := map[CapabilityID]codec.CanonicalJSON{CapabilityRead: readSchema, CapabilityGlob: globSchema, CapabilityGrep: grepSchema}
	facades := make([]Facade, 0, 6)
	for _, capability := range []CapabilityID{CapabilityRead, CapabilityGlob, CapabilityGrep} {
		for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
			name, id := facadeIdentity(family, capability)
			facades = append(facades, Facade{id: id, family: family, name: name, description: descriptions[capability], capability: capability, wireKind: WireStructured, schema: schemas[capability].Clone()})
		}
	}
	return facades, nil
}

// buildFileFacades 构造文件类完整子集：Read/Glob/Grep/Patch/Write。
func buildFileFacades() ([]Facade, error) {
	readOnly, err := buildReadOnlyFacades()
	if err != nil {
		return nil, err
	}
	editSchema, err := buildEditSchema()
	if err != nil {
		return nil, err
	}
	patchFacades := []Facade{
		{id: FacadeAnthropicEdit, family: domain.ProviderAnthropic, name: "Edit", description: "Edits one existing UTF-8 workspace file after showing the exact diff for approval.", capability: CapabilityPatch, wireKind: WireStructured, schema: editSchema},
		{id: FacadeOpenAIPatch, family: domain.ProviderOpenAI, name: "apply_patch", description: "The `apply_patch` tool edits one existing file. This is a FREEFORM tool, so do not wrap the patch in JSON.", capability: CapabilityPatch, wireKind: WireCustomFreeform, format: FreeformFormat{kind: "grammar", syntax: "lark", definition: applyPatchGrammar()}},
	}
	writeSchema, err := buildWriteSchema()
	if err != nil {
		return nil, err
	}
	facades := append(readOnly, patchFacades...)
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		name, id := facadeIdentity(family, CapabilityWrite)
		facades = append(facades, Facade{id: id, family: family, name: name, description: "Creates or overwrites one UTF-8 workspace file with exact content after approval. Overwriting requires a complete Read delivered in an earlier sample; truncated or same-sample reads do not qualify. Missing parent directories are created atomically. Use Edit or apply_patch for partial changes.", capability: CapabilityWrite, wireKind: WireStructured, schema: writeSchema.Clone()})
	}
	return facades, nil
}

// NewCatalogSnapshot 创建当前六能力唯一目录，要求所有消费者已显式装配。
func NewCatalogSnapshot(readExecutor ReadExecutor, globExecutor GlobExecutor, grepExecutor GrepExecutor, preparer PatchPreparer, patchExecutor PatchExecutor, writePreparer WritePreparer, writeExecutor WriteExecutor, execPreparer ExecPreparer, execExecutor ExecExecutor, renderer ExecOutputRenderer, policy Policy) (CatalogSnapshot, error) {
	if readExecutor == nil || globExecutor == nil || grepExecutor == nil || preparer == nil || patchExecutor == nil || writePreparer == nil || writeExecutor == nil || execPreparer == nil || execExecutor == nil || renderer == nil || policy == nil {
		return CatalogSnapshot{}, errors.New("all tool components are required")
	}
	facades, err := buildFileFacades()
	if err != nil {
		return CatalogSnapshot{}, err
	}
	execSchema, err := buildExecSchema()
	if err != nil {
		return CatalogSnapshot{}, err
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		name, id := facadeIdentity(family, CapabilityExec)
		facades = append(facades, Facade{id: id, family: family, name: name, description: "Runs a foreground command after one-time approval, with workspace writes, no network, bounded output and timeout.", capability: CapabilityExec, wireKind: WireStructured, schema: execSchema.Clone()})
	}
	return newCatalogSnapshot(facades)
}

func buildWriteSchema() (codec.CanonicalJSON, error) {
	type properties struct {
		FilePath stringSchemaProperty `json:"file_path"`
		Content  stringSchemaProperty `json:"content"`
	}
	type schema struct {
		Type                 string     `json:"type"`
		Properties           properties `json:"properties"`
		Required             []string   `json:"required"`
		AdditionalProperties bool       `json:"additionalProperties"`
	}
	return codec.MarshalCanonical(schema{Type: "object", Properties: properties{FilePath: stringSchemaProperty{Type: "string", Description: "Canonical workspace-relative file path (at most 4096 bytes and 64 components)"}, Content: stringSchemaProperty{Type: "string", Description: "Exact UTF-8 file content; empty content is allowed. Input is limited to 1 MiB."}}, Required: []string{"file_path", "content"}, AdditionalProperties: false}, maxCatalogBytes)
}

func buildEditSchema() (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(editInputSchema{
		Type: "object", Properties: editInputSchemaProperties{
			FilePath:   stringSchemaProperty{Type: "string", Description: "Workspace-relative path to an existing UTF-8 file"},
			OldString:  stringSchemaProperty{Type: "string", Description: "Non-empty exact text to replace"},
			NewString:  stringSchemaProperty{Type: "string", Description: "Replacement text"},
			ReplaceAll: booleanSchemaProperty{Type: "boolean", Description: "Replace every exact match instead of requiring exactly one"},
		}, Required: []string{"file_path", "old_string", "new_string"}, AdditionalProperties: false,
	}, maxCatalogBytes)
}

func applyPatchGrammar() string {
	return "start: begin update hunk+ end LF?\n" +
		"begin: \"*** Begin Patch\" LF\n" +
		"update: \"*** Update File: \" path LF\n" +
		"hunk: \"@@\" LF hunk_line+\n" +
		"hunk_line: (\" \" | \"-\" | \"+\") line LF\n" +
		"end: \"*** End Patch\"\n" +
		"path: /[^\\r\\n]+/\nline: /[^\\r\\n]*/\nLF: \"\\n\""
}

func buildReadSchema() (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(readInputSchema{
		Type: "object", Properties: readInputSchemaProperties{
			FilePath: stringSchemaProperty{Type: "string", Description: "The absolute or workspace-relative path to the file to read"},
			Offset:   integerSchemaProperty{Type: "integer", Description: "The 1-based line number to start reading from", Minimum: 1},
			Limit:    integerSchemaProperty{Type: "integer", Description: "The maximum number of lines to read", Minimum: 1, Maximum: maxReadLines},
		}, Required: []string{"file_path"}, AdditionalProperties: false,
	}, maxCatalogBytes)
}

func buildGlobSchema() (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(globInputSchema{
		Type: "object", Properties: globInputSchemaProperties{
			Pattern: stringSchemaProperty{Type: "string", Description: "Workspace-relative glob pattern; ** matches complete path segments"},
			Path:    stringSchemaProperty{Type: "string", Description: "Optional workspace-relative directory to search"},
			Limit:   integerSchemaProperty{Type: "integer", Description: "Maximum number of matches", Minimum: 1, Maximum: maxSearchLimit},
		}, Required: []string{"pattern"}, AdditionalProperties: false,
	}, maxCatalogBytes)
}

func buildGrepSchema() (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(grepInputSchema{
		Type: "object", Properties: grepInputSchemaProperties{
			Pattern:         stringSchemaProperty{Type: "string", Description: "Line-oriented Go RE2 regular expression"},
			Path:            stringSchemaProperty{Type: "string", Description: "Optional workspace-relative directory to search"},
			Glob:            stringSchemaProperty{Type: "string", Description: "Optional workspace-relative glob filter"},
			OutputMode:      enumSchemaProperty{Type: "string", Description: "Result shape", Enum: []string{string(GrepOutputContent), string(GrepOutputFilesWithMatches), string(GrepOutputCount)}},
			CaseInsensitive: booleanSchemaProperty{Type: "boolean", Description: "Use case-insensitive matching"},
			BeforeContext:   integerSchemaProperty{Type: "integer", Description: "Context lines before a match", Minimum: 0, Maximum: maxGrepContext},
			AfterContext:    integerSchemaProperty{Type: "integer", Description: "Context lines after a match", Minimum: 0, Maximum: maxGrepContext},
			Limit:           integerSchemaProperty{Type: "integer", Description: "Maximum number of normalized results", Minimum: 1, Maximum: maxSearchLimit},
		}, Required: []string{"pattern"}, AdditionalProperties: false,
	}, maxCatalogBytes)
}

func newCatalogSnapshot(facades []Facade) (CatalogSnapshot, error) {
	if len(facades) != 12 {
		return CatalogSnapshot{}, errors.New("tool catalog requires a complete Provider facade set")
	}
	owned := cloneFacades(facades)
	for index := range owned {
		if err := owned[index].validate(); err != nil {
			return CatalogSnapshot{}, err
		}
	}
	sortFacades(owned)
	for index := 1; index < len(owned); index++ {
		if owned[index-1].id == owned[index].id || (owned[index-1].family == owned[index].family && owned[index-1].capability == owned[index].capability) {
			return CatalogSnapshot{}, errors.New("duplicate tool facade")
		}
	}
	canonical, err := encodeFacades(owned)
	if err != nil {
		return CatalogSnapshot{}, err
	}
	sum := sha256.Sum256(canonical.Bytes())
	fingerprint := hex.EncodeToString(sum[:])
	return CatalogSnapshot{facades: owned, canonical: canonical, fingerprint: fingerprint}, nil
}

func encodeFacades(facades []Facade) (codec.CanonicalJSON, error) {
	canonicalFacades := make([]facadeCanonical, len(facades))
	for index, facade := range facades {
		canonicalFacades[index] = facadeCanonical{
			ID: facade.id, Family: facade.family, Name: facade.name, Description: facade.description,
			Capability: facade.capability, WireKind: facade.wireKind, InputSchema: json.RawMessage(facade.schema.Bytes()),
		}
		if !facade.format.zero() {
			canonicalFacades[index].Format = &freeformFormatCanonical{Type: facade.format.kind, Syntax: facade.format.syntax, Definition: facade.format.definition}
		}
	}
	canonical, err := codec.MarshalCanonical(catalogCanonical{Facades: canonicalFacades}, maxCatalogBytes)
	if err != nil {
		return codec.CanonicalJSON{}, fmt.Errorf("encode tool catalog: %w", err)
	}
	return canonical, nil
}

func (snapshot CatalogSnapshot) CanonicalJSON() []byte { return snapshot.canonical.Bytes() }
func (snapshot CatalogSnapshot) Fingerprint() string   { return snapshot.fingerprint }
func (snapshot CatalogSnapshot) Facades() []Facade     { return cloneFacades(snapshot.facades) }
func (snapshot CatalogSnapshot) Clone() CatalogSnapshot {
	return CatalogSnapshot{facades: snapshot.Facades(), canonical: snapshot.canonical.Clone(), fingerprint: snapshot.fingerprint}
}

// View 返回指定 Provider family 的独立 facade 快照。
func (snapshot CatalogSnapshot) View(family domain.ProviderFamily) (FacadeView, error) {
	if err := snapshot.Validate(); err != nil {
		return FacadeView{}, err
	}
	var selected []Facade
	for _, facade := range snapshot.facades {
		if facade.family == family {
			selected = append(selected, facade.clone())
		}
	}
	if len(selected) != 6 {
		return FacadeView{}, errors.New("tool catalog has incomplete Provider facade set")
	}
	canonical, err := encodeFacades(selected)
	if err != nil {
		return FacadeView{}, err
	}
	sum := sha256.Sum256(canonical.Bytes())
	return FacadeView{family: family, facades: selected, canonical: canonical, fingerprint: hex.EncodeToString(sum[:])}, nil
}

// DecodeReadyCall 按当前 snapshot 的 facade 解码模型参数为封闭联合。
func (snapshot CatalogSnapshot) DecodeReadyCall(family domain.ProviderFamily, name string, callID ProviderCallID, arguments []byte) (ReadyCall, error) {
	view, err := snapshot.View(family)
	if err != nil {
		return ReadyCall{}, err
	}
	var selected *Facade
	for index := range view.facades {
		if view.facades[index].name == name {
			selected = &view.facades[index]
			break
		}
	}
	if selected == nil {
		return ReadyCall{}, errors.New("tool facade is not advertised")
	}
	switch selected.capability {
	case CapabilityExec:
		input, decodeErr := DecodeExecInput(arguments)
		if decodeErr != nil {
			return ReadyCall{}, decodeErr
		}
		return NewExecReadyCall(callID, input)
	case CapabilityRead:
		input, decodeErr := DecodeReadInput(arguments)
		if decodeErr != nil {
			return ReadyCall{}, decodeErr
		}
		return NewReadReadyCall(callID, input)
	case CapabilityGlob:
		input, decodeErr := DecodeGlobInput(arguments)
		if decodeErr != nil {
			return ReadyCall{}, decodeErr
		}
		return NewGlobReadyCall(callID, input)
	case CapabilityGrep:
		input, decodeErr := DecodeGrepInput(arguments)
		if decodeErr != nil {
			return ReadyCall{}, decodeErr
		}
		return NewGrepReadyCall(callID, input)
	case CapabilityWrite:
		input, err := DecodeWriteInput(arguments)
		if err != nil {
			return ReadyCall{}, err
		}
		return NewWriteReadyCall(callID, input)
	case CapabilityPatch:
		var input PatchInput
		var decodeErr error
		if selected.wireKind == WireStructured {
			input, decodeErr = DecodeAnthropicEditInput(arguments)
		} else {
			input, decodeErr = ParseOpenAIApplyPatchInput(arguments)
		}
		if decodeErr != nil {
			return ReadyCall{}, decodeErr
		}
		return NewPatchReadyCall(callID, selected.wireKind, input)
	default:
		return ReadyCall{}, errors.New("tool facade capability is unsupported")
	}
}

// Validate 重新计算 canonical bytes 与 fingerprint。
func (snapshot CatalogSnapshot) Validate() error {
	if len(snapshot.facades) != 12 || snapshot.fingerprint == "" {
		return errors.New("tool catalog snapshot is invalid")
	}
	rebuilt, err := newCatalogSnapshot(snapshot.facades)
	if err != nil {
		return err
	}
	if !bytes.Equal(snapshot.canonical.Bytes(), rebuilt.canonical.Bytes()) || snapshot.fingerprint != rebuilt.fingerprint {
		return errors.New("tool catalog snapshot does not match fields")
	}
	return nil
}

func cloneFacades(source []Facade) []Facade {
	result := make([]Facade, len(source))
	for index := range source {
		result[index] = source[index].clone()
	}
	return result
}

func sortFacades(facades []Facade) {
	sort.Slice(facades, func(left int, right int) bool {
		if facades[left].capability != facades[right].capability {
			return facades[left].capability < facades[right].capability
		}
		if facades[left].name != facades[right].name {
			return facades[left].name < facades[right].name
		}
		return facades[left].family < facades[right].family
	})
}
