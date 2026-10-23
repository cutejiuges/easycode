package tool

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DirectoryEvidence 绑定从工作区根到最后已有目录的实际句柄身份。
type DirectoryEvidence struct {
	relativePath string
	device       uint64
	inode        uint64
	mode         uint32
}

// NewDirectoryEvidence 构造目录证据，不打开目录。
func NewDirectoryEvidence(path string, device, inode uint64, mode uint32) (DirectoryEvidence, error) {
	e := DirectoryEvidence{relativePath: path, device: device, inode: inode, mode: mode}
	return e, e.Validate()
}

func (e DirectoryEvidence) RelativePath() string { return e.relativePath }
func (e DirectoryEvidence) Device() uint64       { return e.device }
func (e DirectoryEvidence) Inode() uint64        { return e.inode }
func (e DirectoryEvidence) Mode() uint32         { return e.mode }

// Validate 校验目录证据的有界路径和身份。
func (e DirectoryEvidence) Validate() error {
	if (e.relativePath != "." && validateWritePath(e.relativePath) != nil) || e.device == 0 || e.inode == 0 || e.mode > 0o7777 {
		return errors.New("invalid directory evidence")
	}
	return nil
}

// WriteCandidate 冻结一次写入的操作、证据、目录计划和完整审批展示。
type WriteCandidate struct {
	invocation  WriteInvocation
	operation   WriteOperation
	base        FileBaseEvidence
	ancestors   []DirectoryEvidence
	directories []string
	preview     string
	fingerprint CandidateFingerprint
	summary     WriteResultMetadata
}

// NewWriteCandidate 构造并深拷贝经过只读准备的候选。
func NewWriteCandidate(v WriteInvocation, operation WriteOperation, base FileBaseEvidence, ancestors []DirectoryEvidence, directories []string, preview string) (WriteCandidate, error) {
	c := WriteCandidate{invocation: v, operation: operation, base: base, ancestors: append([]DirectoryEvidence(nil), ancestors...), directories: append([]string(nil), directories...), preview: preview}
	c.summary, _ = NewWriteResultMetadata(v.input.filePath, operation, len(v.input.content), WriteLineCount(v.input.content), len(directories))
	c.fingerprint = c.computeFingerprint()
	return c, c.Validate()
}

func (c WriteCandidate) Invocation() WriteInvocation    { return c.invocation }
func (c WriteCandidate) InvocationID() InvocationID     { return c.invocation.invocationID }
func (c WriteCandidate) ProviderCallID() ProviderCallID { return c.invocation.providerCallID }
func (c WriteCandidate) Operation() WriteOperation      { return c.operation }
func (c WriteCandidate) RelativePath() string           { return c.invocation.input.filePath }
func (c WriteCandidate) Evidence() FileBaseEvidence     { return c.base }
func (c WriteCandidate) Ancestors() []DirectoryEvidence {
	return append([]DirectoryEvidence(nil), c.ancestors...)
}
func (c WriteCandidate) Directories() []string               { return append([]string(nil), c.directories...) }
func (c WriteCandidate) FinalBytes() []byte                  { return []byte(c.invocation.input.content) }
func (c WriteCandidate) Preview() string                     { return c.preview }
func (c WriteCandidate) Fingerprint() CandidateFingerprint   { return c.fingerprint }
func (c WriteCandidate) ResultMetadata() WriteResultMetadata { return c.summary }

// Mode 返回唯一受支持的权限计划，不承诺复制 ACL 或扩展属性。
func (c WriteCandidate) Mode() uint32 {
	if c.operation == WriteOverwrite {
		return c.base.mode & 0o777
	}
	return 0o600
}

// Validate 验证候选各分支和完整目录计划，不执行 I/O。
func (c WriteCandidate) Validate() error {
	if c.invocation.Validate() != nil || (c.operation != WriteCreate && c.operation != WriteOverwrite) || c.summary.Validate() != nil {
		return errors.New("invalid Write candidate")
	}
	if c.operation == WriteCreate && c.base != (FileBaseEvidence{}) || c.operation == WriteOverwrite && (c.base.Validate() != nil || c.base.mode&0o7000 != 0 || len(c.directories) != 0) {
		return errors.New("Write operation and evidence mismatch")
	}
	parts := strings.Split(c.RelativePath(), "/")
	if len(c.ancestors) == 0 || len(c.directories) > MaxWriteDirectories || len(c.ancestors)+len(c.directories) != len(parts) {
		return errors.New("invalid Write directory plan")
	}
	for i, e := range c.ancestors {
		path := "."
		if i > 0 {
			path = strings.Join(parts[:i], "/")
		}
		if e.Validate() != nil || e.relativePath != path {
			return errors.New("invalid Write ancestor evidence")
		}
	}
	for i, path := range c.directories {
		if path != strings.Join(parts[:len(c.ancestors)+i], "/") {
			return errors.New("invalid Write directory sequence")
		}
	}
	budget := WritePresentationBytes(c.preview, c.summary, c.directories)
	if c.preview == "" || !utf8.ValidString(c.preview) || budget > MaxApprovalDiffBytes {
		return errors.New("invalid Write presentation")
	}
	if c.summary.relativePath != c.RelativePath() || c.summary.operation != c.operation || c.summary.bytes != len(c.invocation.input.content) || c.summary.lines != WriteLineCount(c.invocation.input.content) || c.summary.createdDirectories != len(c.directories) || c.fingerprint != c.computeFingerprint() {
		return errors.New("Write candidate binding mismatch")
	}
	return nil
}

func (c WriteCandidate) computeFingerprint() CandidateFingerprint {
	var data []byte
	field := func(value string) {
		data = binary.BigEndian.AppendUint64(data, uint64(len(value)))
		data = append(data, value...)
	}
	number := func(value uint64) { data = binary.BigEndian.AppendUint64(data, value) }
	field(string(CapabilityWrite))
	field(string(c.operation))
	field(c.RelativePath())
	field(string(c.base.contentDigest))
	number(uint64(c.base.size))
	number(uint64(c.base.mode))
	number(c.base.device)
	number(c.base.inode)
	number(uint64(len(c.ancestors)))
	for _, e := range c.ancestors {
		field(e.relativePath)
		number(e.device)
		number(e.inode)
		number(uint64(e.mode))
	}
	number(uint64(len(c.directories)))
	for _, path := range c.directories {
		field(path)
		number(0o700)
	}
	number(uint64(c.Mode()))
	field(string(NewContentDigest([]byte(c.invocation.input.content))))
	return NewCandidateFingerprint(data)
}

// WritePresentationBytes 计算完整展示预算，包含计数、目录标签和可见转义。
func WritePresentationBytes(preview string, summary WriteResultMetadata, directories []string) int {
	budget := len(preview) + len(fmt.Sprintf("%d bytes, %d lines\n", summary.bytes, summary.lines))
	for _, path := range directories {
		budget += len("Create directory: \n")
		for _, r := range path {
			if r == '\\' {
				budget += 2
			} else if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				budget += len(fmt.Sprintf(`\u{%04X}`, r))
			} else {
				budget += utf8.RuneLen(r)
			}
		}
	}
	return budget
}
