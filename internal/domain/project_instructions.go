package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	ProjectInstructionsSchemaRevision   = "project-instructions-v1"
	ProjectInstructionsRendererRevision = "project-instructions-prompt-v1"
	// MaxProjectInstructionsBytes 限制项目指令最终模型可见文本的字节数。
	MaxProjectInstructionsBytes          = 4 << 20
	maxProjectInstructionsCanonicalBytes = 32 << 20
)

const projectInstructionsHeader = "# Project instructions\n\nFollow the instructions from these project files. Later files are more specific.\n"

// ProjectInstructionDocument 保存一份项目指令及其项目根相对来源。
type ProjectInstructionDocument struct {
	source  string
	content string
}

// NewProjectInstructionDocument 创建经过严格校验的项目指令文档。
func NewProjectInstructionDocument(source string, content string) (ProjectInstructionDocument, error) {
	document := ProjectInstructionDocument{source: source, content: content}
	if err := document.Validate(); err != nil {
		return ProjectInstructionDocument{}, err
	}
	return document, nil
}

// Source 返回使用斜杠分隔的项目根相对来源。
func (document ProjectInstructionDocument) Source() string { return document.source }

// Content 返回不可变的 UTF-8 指令正文。
func (document ProjectInstructionDocument) Content() string { return document.content }

// Validate 校验来源与正文不变量。
func (document ProjectInstructionDocument) Validate() error {
	if document.source == "" || !utf8.ValidString(document.source) {
		return fmt.Errorf("project instruction source is invalid")
	}
	if strings.ContainsAny(document.source, "\\\x00") || path.IsAbs(document.source) ||
		path.Clean(document.source) != document.source || document.source == "." {
		return fmt.Errorf("project instruction source must be a normalized relative path")
	}
	for _, component := range strings.Split(document.source, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("project instruction source contains an invalid component")
		}
	}
	name := path.Base(document.source)
	if name != "AGENTS.md" && name != "CLAUDE.md" {
		return fmt.Errorf("project instruction source filename is unsupported")
	}
	if !utf8.ValidString(document.content) {
		return fmt.Errorf("project instruction content is not valid UTF-8")
	}
	return nil
}

// ProjectInstructionsSnapshot 保存一次进程启动时的不可变项目指令快照。
type ProjectInstructionsSnapshot struct {
	documents     []ProjectInstructionDocument
	truncated     bool
	byteLimit     int
	rendered      string
	revision      string
	canonicalJSON []byte
}

type projectInstructionDocumentWire struct {
	Source  string `json:"source"`
	Content string `json:"content"`
}

type projectInstructionsSnapshotWire struct {
	SchemaRevision   string                           `json:"schema_revision"`
	RendererRevision string                           `json:"renderer_revision"`
	Documents        []projectInstructionDocumentWire `json:"documents"`
	Truncated        bool                             `json:"truncated"`
	ByteLimit        int                              `json:"byte_limit"`
}

// NewProjectInstructionsSnapshot 按模型可见字节上限构造确定性快照。
func NewProjectInstructionsSnapshot(
	documents []ProjectInstructionDocument,
	byteLimit int,
) (ProjectInstructionsSnapshot, error) {
	if err := validateProjectInstructionsByteLimit(byteLimit); err != nil {
		return ProjectInstructionsSnapshot{}, err
	}
	if err := validateProjectInstructionOrder(documents); err != nil {
		return ProjectInstructionsSnapshot{}, err
	}
	bounded, rendered, truncated := renderBoundedProjectInstructions(documents, byteLimit)
	return buildProjectInstructionsSnapshot(bounded, truncated, byteLimit, rendered)
}

// NewEmptyProjectInstructionsSnapshot 创建不产生 Provider 上下文项的有效空快照。
func NewEmptyProjectInstructionsSnapshot(byteLimit int) (ProjectInstructionsSnapshot, error) {
	return NewProjectInstructionsSnapshot(nil, byteLimit)
}

// Documents 返回不与快照共享切片的文档副本。
func (snapshot ProjectInstructionsSnapshot) Documents() []ProjectInstructionDocument {
	return append([]ProjectInstructionDocument(nil), snapshot.documents...)
}

// HasDocuments 判断快照是否包含可注入的项目指令。
func (snapshot ProjectInstructionsSnapshot) HasDocuments() bool { return len(snapshot.documents) > 0 }

// Truncated 判断快照是否因模型可见字节预算发生截断。
func (snapshot ProjectInstructionsSnapshot) Truncated() bool { return snapshot.truncated }

// ByteLimit 返回构造快照时使用的模型可见字节上限。
func (snapshot ProjectInstructionsSnapshot) ByteLimit() int { return snapshot.byteLimit }

// RenderedText 返回 Provider 请求使用的确定性文本。
func (snapshot ProjectInstructionsSnapshot) RenderedText() string { return snapshot.rendered }

// Revision 返回结构化快照 canonical bytes 的 SHA-256 revision。
func (snapshot ProjectInstructionsSnapshot) Revision() string { return snapshot.revision }

// CanonicalJSON 返回不与快照共享底层 buffer 的 canonical JSON。
func (snapshot ProjectInstructionsSnapshot) CanonicalJSON() []byte {
	return append([]byte(nil), snapshot.canonicalJSON...)
}

// Clone 返回与当前快照不共享可变内存的副本。
func (snapshot ProjectInstructionsSnapshot) Clone() (ProjectInstructionsSnapshot, error) {
	if err := snapshot.Validate(); err != nil {
		return ProjectInstructionsSnapshot{}, err
	}
	clone := snapshot
	clone.documents = snapshot.Documents()
	clone.canonicalJSON = snapshot.CanonicalJSON()
	return clone, nil
}

// Validate 校验快照的文档、渲染结果、canonical bytes 与 revision。
func (snapshot ProjectInstructionsSnapshot) Validate() error {
	if err := validateProjectInstructionsByteLimit(snapshot.byteLimit); err != nil {
		return err
	}
	if err := validateProjectInstructionOrder(snapshot.documents); err != nil {
		return err
	}
	if len(snapshot.rendered) > snapshot.byteLimit {
		return fmt.Errorf("project instruction rendering exceeds the byte limit")
	}
	if len(snapshot.documents) == 0 && snapshot.rendered != "" {
		return fmt.Errorf("empty project instructions contain rendered text")
	}
	wantRendered := renderProjectInstructions(snapshot.documents)
	if snapshot.rendered != wantRendered {
		return fmt.Errorf("project instruction rendering does not match documents")
	}
	wire := projectInstructionsWire(snapshot.documents, snapshot.truncated, snapshot.byteLimit)
	wantCanonical, err := marshalProjectInstructionsCanonical(wire)
	if err != nil {
		return fmt.Errorf("project instruction canonical snapshot is invalid: %w", err)
	}
	if !bytes.Equal(snapshot.canonicalJSON, wantCanonical) {
		return fmt.Errorf("project instruction canonical snapshot does not match fields")
	}
	sum := sha256.Sum256(wantCanonical)
	if snapshot.revision != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("project instruction revision does not match canonical snapshot")
	}
	return nil
}

func validateProjectInstructionsByteLimit(byteLimit int) error {
	if byteLimit <= 0 {
		return fmt.Errorf("project instruction byte limit must be positive")
	}
	if byteLimit > MaxProjectInstructionsBytes {
		return fmt.Errorf("project instruction byte limit exceeds %d bytes", MaxProjectInstructionsBytes)
	}
	return nil
}

func buildProjectInstructionsSnapshot(
	documents []ProjectInstructionDocument,
	truncated bool,
	byteLimit int,
	rendered string,
) (ProjectInstructionsSnapshot, error) {
	wire := projectInstructionsWire(documents, truncated, byteLimit)
	canonical, err := marshalProjectInstructionsCanonical(wire)
	if err != nil {
		return ProjectInstructionsSnapshot{}, fmt.Errorf("encode project instruction snapshot: %w", err)
	}
	sum := sha256.Sum256(canonical)
	snapshot := ProjectInstructionsSnapshot{
		documents: append([]ProjectInstructionDocument(nil), documents...),
		truncated: truncated, byteLimit: byteLimit, rendered: rendered,
		revision: hex.EncodeToString(sum[:]), canonicalJSON: append([]byte(nil), canonical...),
	}
	if err := snapshot.Validate(); err != nil {
		return ProjectInstructionsSnapshot{}, err
	}
	return snapshot, nil
}

func marshalProjectInstructionsCanonical(wire projectInstructionsSnapshotWire) ([]byte, error) {
	encoded, err := codec.MarshalCanonical(wire, maxProjectInstructionsCanonicalBytes)
	if err != nil {
		return nil, fmt.Errorf("marshal project instruction snapshot: %w", err)
	}
	return encoded.Bytes(), nil
}

func projectInstructionsWire(
	documents []ProjectInstructionDocument,
	truncated bool,
	byteLimit int,
) projectInstructionsSnapshotWire {
	wireDocuments := make([]projectInstructionDocumentWire, len(documents))
	for index, document := range documents {
		wireDocuments[index] = projectInstructionDocumentWire{Source: document.source, Content: document.content}
	}
	return projectInstructionsSnapshotWire{
		SchemaRevision:   ProjectInstructionsSchemaRevision,
		RendererRevision: ProjectInstructionsRendererRevision,
		Documents:        wireDocuments, Truncated: truncated, ByteLimit: byteLimit,
	}
}

func validateProjectInstructionOrder(documents []ProjectInstructionDocument) error {
	seen := make(map[string]struct{}, len(documents))
	previousDirectory := "."
	for index, document := range documents {
		if err := document.Validate(); err != nil {
			return err
		}
		if _, exists := seen[document.source]; exists {
			return fmt.Errorf("duplicate project instruction source")
		}
		seen[document.source] = struct{}{}
		directory := path.Dir(document.source)
		if index > 0 && (directory == previousDirectory ||
			!sameOrDescendantProjectDirectory(previousDirectory, directory)) {
			return fmt.Errorf("project instruction sources are not ordered from root to startup directory")
		}
		previousDirectory = directory
	}
	return nil
}

func sameOrDescendantProjectDirectory(parent string, child string) bool {
	if parent == "." {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func renderBoundedProjectInstructions(
	documents []ProjectInstructionDocument,
	byteLimit int,
) ([]ProjectInstructionDocument, string, bool) {
	if len(documents) == 0 {
		return make([]ProjectInstructionDocument, 0), "", false
	}
	remaining := byteLimit
	if len(projectInstructionsHeader) > remaining {
		return make([]ProjectInstructionDocument, 0), "", true
	}
	remaining -= len(projectInstructionsHeader)
	bounded := make([]ProjectInstructionDocument, 0, len(documents))
	truncated := false
	for _, document := range documents {
		prefix, suffix := projectInstructionDocumentBoundary(document.source)
		if len(prefix)+len(suffix) > remaining {
			truncated = true
			break
		}
		available := remaining - len(prefix) - len(suffix)
		content := document.content
		if len(content) > available {
			content = truncateUTF8(content, available)
			truncated = true
		}
		bounded = append(bounded, ProjectInstructionDocument{source: document.source, content: content})
		remaining -= len(prefix) + len(content) + len(suffix)
		if truncated {
			break
		}
	}
	if len(bounded) < len(documents) {
		truncated = true
	}
	return bounded, renderProjectInstructions(bounded), truncated
}

func renderProjectInstructions(documents []ProjectInstructionDocument) string {
	if len(documents) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(projectInstructionsHeader)
	for _, document := range documents {
		prefix, suffix := projectInstructionDocumentBoundary(document.source)
		builder.WriteString(prefix)
		builder.WriteString(document.content)
		builder.WriteString(suffix)
	}
	return builder.String()
}

func projectInstructionDocumentBoundary(source string) (string, string) {
	return "\n## Source: " + strconv.Quote(source) + "\n\n<INSTRUCTIONS>\n", "\n</INSTRUCTIONS>\n"
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
