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

	FacadeAnthropicRead FacadeID = "anthropic:Read"
	FacadeOpenAIRead    FacadeID = "openai:Read"
	FacadeAnthropicGlob FacadeID = "anthropic:Glob"
	FacadeOpenAIGlob    FacadeID = "openai:Glob"
	FacadeAnthropicGrep FacadeID = "anthropic:Grep"
	FacadeOpenAIGrep    FacadeID = "openai:Grep"
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
	schema      codec.CanonicalJSON
}

func (facade Facade) ID() FacadeID                  { return facade.id }
func (facade Facade) Family() domain.ProviderFamily { return facade.family }
func (facade Facade) Name() string                  { return facade.name }
func (facade Facade) Description() string           { return facade.description }
func (facade Facade) Capability() CapabilityID      { return facade.capability }
func (facade Facade) InputSchema() []byte           { return facade.schema.Bytes() }

func (facade Facade) clone() Facade {
	facade.schema = facade.schema.Clone()
	return facade
}

func (facade Facade) validate() error {
	if !facade.family.Valid() || facade.name == "" || facade.description == "" || !facade.capability.Valid() || facade.schema.Len() == 0 {
		return errors.New("invalid tool facade")
	}
	wantName, wantID := facadeIdentity(facade.family, facade.capability)
	if facade.name != wantName || facade.id != wantID {
		return errors.New("tool facade identity does not match capability")
	}
	return facade.schema.Validate(maxCatalogBytes)
}

func facadeIdentity(family domain.ProviderFamily, capability CapabilityID) (string, FacadeID) {
	prefix := "openai:"
	if family == domain.ProviderAnthropic {
		prefix = "anthropic:"
	}
	switch capability {
	case CapabilityRead:
		return "Read", FacadeID(prefix + "Read")
	case CapabilityGlob:
		return "Glob", FacadeID(prefix + "Glob")
	case CapabilityGrep:
		return "Grep", FacadeID(prefix + "Grep")
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

// CatalogSnapshot 保存只含真实 Read、Glob、Grep 能力的确定性目录。
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

type facadeCanonical struct {
	ID          FacadeID              `json:"id"`
	Family      domain.ProviderFamily `json:"family"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Capability  CapabilityID          `json:"capability"`
	InputSchema json.RawMessage       `json:"input_schema"`
}

type catalogCanonical struct {
	Facades []facadeCanonical `json:"facades"`
}

// NewReadOnlyCatalogSnapshot 创建同时供 Anthropic 与 OpenAI 使用的完整只读目录。
func NewReadOnlyCatalogSnapshot(readExecutor ReadExecutor, globExecutor GlobExecutor, grepExecutor GrepExecutor) (CatalogSnapshot, error) {
	if readExecutor == nil || globExecutor == nil || grepExecutor == nil {
		return CatalogSnapshot{}, errors.New("all read-only executors are required")
	}
	readSchema, err := buildReadSchema()
	if err != nil {
		return CatalogSnapshot{}, err
	}
	globSchema, err := buildGlobSchema()
	if err != nil {
		return CatalogSnapshot{}, err
	}
	grepSchema, err := buildGrepSchema()
	if err != nil {
		return CatalogSnapshot{}, err
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
			facades = append(facades, Facade{id: id, family: family, name: name, description: descriptions[capability], capability: capability, schema: schemas[capability].Clone()})
		}
	}
	return newCatalogSnapshot(facades)
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
	if len(facades) != 6 {
		return CatalogSnapshot{}, errors.New("read-only catalog requires six Provider facades")
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
			Capability: facade.capability, InputSchema: json.RawMessage(facade.schema.Bytes()),
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
	if len(selected) != 3 {
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
	default:
		return ReadyCall{}, errors.New("tool facade capability is unsupported")
	}
}

// Validate 重新计算 canonical bytes 与 fingerprint。
func (snapshot CatalogSnapshot) Validate() error {
	if len(snapshot.facades) != 6 || snapshot.fingerprint == "" {
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
