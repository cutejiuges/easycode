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
	CatalogRevision = "tool-catalog.v1"
	maxCatalogBytes = 1 << 20

	FacadeAnthropicRead FacadeID = "anthropic:Read"
	FacadeOpenAIRead    FacadeID = "openai:Read"
)

// FacadeID 唯一标识某个 Provider family 的模型可见 facade。
type FacadeID string

// Facade 是 Provider 请求编译所需的不可变视图。
type Facade struct {
	id          FacadeID
	family      domain.ProviderFamily
	name        string
	description string
	schema      codec.CanonicalJSON
}

func (facade Facade) ID() FacadeID                  { return facade.id }
func (facade Facade) Family() domain.ProviderFamily { return facade.family }
func (facade Facade) Name() string                  { return facade.name }
func (facade Facade) Description() string           { return facade.description }
func (facade Facade) Capability() CapabilityID      { return CapabilityRead }
func (facade Facade) InputRevision() string         { return ReadInputRevision }
func (facade Facade) ResultCodecRevision() string   { return ReadResultCodecRevision }
func (facade Facade) InputSchema() []byte           { return facade.schema.Bytes() }

func (facade Facade) clone() Facade {
	facade.schema = facade.schema.Clone()
	return facade
}

func (facade Facade) validate() error {
	if !facade.family.Valid() || facade.name != "Read" || facade.description == "" || facade.schema.Len() == 0 {
		return errors.New("invalid Read facade")
	}
	if facade.family == domain.ProviderAnthropic && facade.id != FacadeAnthropicRead {
		return errors.New("invalid Anthropic Read facade ID")
	}
	if facade.family == domain.ProviderOpenAI && facade.id != FacadeOpenAIRead {
		return errors.New("invalid OpenAI Read facade ID")
	}
	return facade.schema.Validate(maxCatalogBytes)
}

// FacadeView 是单个 Provider family 的稳定 facade 快照。
type FacadeView struct {
	family      domain.ProviderFamily
	facades     []Facade
	canonical   codec.CanonicalJSON
	fingerprint string
}

func (view FacadeView) Family() domain.ProviderFamily { return view.family }
func (view FacadeView) Facades() []Facade {
	result := make([]Facade, len(view.facades))
	for index := range view.facades {
		result[index] = view.facades[index].clone()
	}
	return result
}
func (view FacadeView) CanonicalJSON() []byte { return view.canonical.Bytes() }
func (view FacadeView) Fingerprint() string   { return view.fingerprint }

// CatalogSnapshot 保存只含真实 Read 能力的确定性目录。
type CatalogSnapshot struct {
	revision    string
	facades     []Facade
	canonical   codec.CanonicalJSON
	fingerprint string
}

type readInputSchema struct {
	Type                 string                    `json:"type"`
	Properties           readInputSchemaProperties `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type readInputSchemaProperties struct {
	FilePath readStringProperty  `json:"file_path"`
	Offset   readIntegerProperty `json:"offset"`
	Limit    readIntegerProperty `json:"limit"`
}

type readStringProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type readIntegerProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Minimum     int    `json:"minimum"`
	Maximum     int    `json:"maximum,omitempty"`
}

type facadeCanonical struct {
	ID                  FacadeID              `json:"id"`
	Family              domain.ProviderFamily `json:"family"`
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	Capability          CapabilityID          `json:"capability"`
	InputRevision       string                `json:"input_revision"`
	ResultCodecRevision string                `json:"result_codec_revision"`
	InputSchema         json.RawMessage       `json:"input_schema"`
}

type catalogCanonical struct {
	Revision string            `json:"revision"`
	Facades  []facadeCanonical `json:"facades"`
}

// NewReadCatalogSnapshot 创建同时供 Anthropic 与 OpenAI 使用的 Read-only catalog。
func NewReadCatalogSnapshot(executor ReadExecutor) (CatalogSnapshot, error) {
	if executor == nil {
		return CatalogSnapshot{}, errors.New("read executor is required")
	}
	schema, err := codec.MarshalCanonical(readInputSchema{
		Type: "object",
		Properties: readInputSchemaProperties{
			FilePath: readStringProperty{Type: "string", Description: "The absolute or workspace-relative path to the file to read"},
			Offset:   readIntegerProperty{Type: "integer", Description: "The 1-based line number to start reading from", Minimum: 1},
			Limit:    readIntegerProperty{Type: "integer", Description: "The maximum number of lines to read", Minimum: 1, Maximum: maxReadLines},
		},
		Required: []string{"file_path"}, AdditionalProperties: false,
	}, maxCatalogBytes)
	if err != nil {
		return CatalogSnapshot{}, fmt.Errorf("build Read schema: %w", err)
	}
	description := "Reads a UTF-8 text file from the local workspace. Returns up to 2000 lines with stable 1-based line numbers; use offset and limit for large files."
	return newCatalogSnapshot(CatalogRevision, []Facade{
		{id: FacadeOpenAIRead, family: domain.ProviderOpenAI, name: "Read", description: description, schema: schema.Clone()},
		{id: FacadeAnthropicRead, family: domain.ProviderAnthropic, name: "Read", description: description, schema: schema.Clone()},
	})
}

func newCatalogSnapshot(revision string, facades []Facade) (CatalogSnapshot, error) {
	if revision == "" {
		return CatalogSnapshot{}, errors.New("tool catalog revision is required")
	}
	if len(facades) != 2 {
		return CatalogSnapshot{}, errors.New("read catalog requires exactly two Provider facades")
	}
	owned := make([]Facade, len(facades))
	for index := range facades {
		if err := facades[index].validate(); err != nil {
			return CatalogSnapshot{}, err
		}
		owned[index] = facades[index].clone()
	}
	sort.Slice(owned, func(left int, right int) bool {
		if owned[left].family != owned[right].family {
			return owned[left].family < owned[right].family
		}
		return owned[left].id < owned[right].id
	})
	if owned[0].family == owned[1].family {
		return CatalogSnapshot{}, errors.New("duplicate Read Provider facade")
	}
	canonicalFacades := make([]facadeCanonical, len(owned))
	for index, facade := range owned {
		canonicalFacades[index] = facadeCanonical{
			ID: facade.id, Family: facade.family, Name: facade.name, Description: facade.description,
			Capability: CapabilityRead, InputRevision: ReadInputRevision,
			ResultCodecRevision: ReadResultCodecRevision, InputSchema: json.RawMessage(facade.schema.Bytes()),
		}
	}
	canonical, err := codec.MarshalCanonical(catalogCanonical{Revision: revision, Facades: canonicalFacades}, maxCatalogBytes)
	if err != nil {
		return CatalogSnapshot{}, fmt.Errorf("encode tool catalog: %w", err)
	}
	sum := sha256.Sum256(canonical.Bytes())
	return CatalogSnapshot{revision: revision, facades: owned, canonical: canonical, fingerprint: hex.EncodeToString(sum[:])}, nil
}

func (snapshot CatalogSnapshot) Revision() string      { return snapshot.revision }
func (snapshot CatalogSnapshot) CanonicalJSON() []byte { return snapshot.canonical.Bytes() }
func (snapshot CatalogSnapshot) Fingerprint() string   { return snapshot.fingerprint }

// Clone 返回不共享 facade slice 和 canonical bytes 的快照。
func (snapshot CatalogSnapshot) Clone() CatalogSnapshot {
	return CatalogSnapshot{
		revision: snapshot.revision, facades: snapshot.Facades(), canonical: snapshot.canonical.Clone(), fingerprint: snapshot.fingerprint,
	}
}
func (snapshot CatalogSnapshot) Facades() []Facade {
	result := make([]Facade, len(snapshot.facades))
	for index := range snapshot.facades {
		result[index] = snapshot.facades[index].clone()
	}
	return result
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
	if len(selected) != 1 {
		return FacadeView{}, errors.New("tool catalog has no facade for Provider family")
	}
	wire := catalogCanonical{Revision: snapshot.revision, Facades: []facadeCanonical{{
		ID: selected[0].id, Family: selected[0].family, Name: selected[0].name,
		Description: selected[0].description, Capability: CapabilityRead,
		InputRevision: ReadInputRevision, ResultCodecRevision: ReadResultCodecRevision,
		InputSchema: json.RawMessage(selected[0].schema.Bytes()),
	}}}
	canonical, err := codec.MarshalCanonical(wire, maxCatalogBytes)
	if err != nil {
		return FacadeView{}, fmt.Errorf("encode Provider tool catalog view: %w", err)
	}
	sum := sha256.Sum256(canonical.Bytes())
	return FacadeView{family: family, facades: selected, canonical: canonical, fingerprint: hex.EncodeToString(sum[:])}, nil
}

// DecodeRead 按当前 snapshot 的 facade 解码模型参数。
func (snapshot CatalogSnapshot) DecodeRead(family domain.ProviderFamily, name string, arguments []byte) (ReadInput, error) {
	view, err := snapshot.View(family)
	if err != nil {
		return ReadInput{}, err
	}
	if len(view.facades) != 1 || name != view.facades[0].name {
		return ReadInput{}, errors.New("tool facade is not advertised")
	}
	return DecodeReadInput(arguments)
}

// Validate 重新计算 canonical bytes 与 fingerprint，防止零值或损坏快照流入请求。
func (snapshot CatalogSnapshot) Validate() error {
	if len(snapshot.facades) != 2 || snapshot.fingerprint == "" {
		return errors.New("tool catalog snapshot is invalid")
	}
	rebuilt, err := newCatalogSnapshot(snapshot.revision, snapshot.facades)
	if err != nil {
		return err
	}
	if !bytes.Equal(snapshot.canonical.Bytes(), rebuilt.canonical.Bytes()) || snapshot.fingerprint != rebuilt.fingerprint {
		return errors.New("tool catalog snapshot does not match fields")
	}
	return nil
}
