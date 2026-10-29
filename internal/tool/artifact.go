package tool

import (
	"encoding/hex"
	"errors"
	"strings"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const (
	// ArtifactStreamBytes 是一个输出流允许保存的原始字节数。
	ArtifactStreamBytes = 16 << 20
	// ArtifactInvocationBytes 是单次命令预留的保存预算。
	ArtifactInvocationBytes = 2 * ArtifactStreamBytes
	// ArtifactThreadBytes 是单个线程的保存预算。
	ArtifactThreadBytes = 256 << 20
	// ArtifactRootBytes 是存储根的跨进程保存预算。
	ArtifactRootBytes = 1 << 30
	// ArtifactPageBytes 是一次读取的最大原始范围。
	ArtifactPageBytes = 64 << 10
)

// ArtifactID 是不包含路径的随机对象身份。
type ArtifactID string

// ParseArtifactID 仅接受规范的小写十六进制身份。
func ParseArtifactID(value string) (ArtifactID, error) {
	if len(value) != 32 || strings.ToLower(value) != value {
		return "", errors.New("invalid artifact ID")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("invalid artifact ID")
	}
	return ArtifactID(value), nil
}

// Valid 验证对象身份。
func (id ArtifactID) Valid() bool { _, err := ParseArtifactID(string(id)); return err == nil }

// ArtifactStream 是保存对象的原始输出流。
type ArtifactStream string

const (
	ArtifactStdout ArtifactStream = "stdout"
	ArtifactStderr ArtifactStream = "stderr"
)

// Valid 只接受两个进程输出流。
func (stream ArtifactStream) Valid() bool {
	return stream == ArtifactStdout || stream == ArtifactStderr
}

// ArtifactDescriptor 是已发布对象的不可变完整性与归属信息。
type ArtifactDescriptor struct {
	id              ArtifactID
	thread          domain.ThreadID
	invocation      InvocationID
	stream          ArtifactStream
	saved, observed int64
	digest          string
	complete        bool
}

// NewArtifactDescriptor 构造描述符，不访问对象或授予读取权限。
func NewArtifactDescriptor(id ArtifactID, thread domain.ThreadID, invocation InvocationID, stream ArtifactStream, saved, observed int64, digest string, complete bool) (ArtifactDescriptor, error) {
	d := ArtifactDescriptor{id, thread, invocation, stream, saved, observed, digest, complete}
	return d, d.Validate()
}

// Validate 验证归属、保存范围和摘要格式。
func (d ArtifactDescriptor) Validate() error {
	if !d.id.Valid() || !d.thread.Valid() || !d.invocation.Valid() || !d.stream.Valid() || d.saved <= 0 || d.saved > ArtifactStreamBytes || d.observed < d.saved || (d.complete && d.saved != d.observed) || len(d.digest) != 64 || strings.ToLower(d.digest) != d.digest {
		return errors.New("invalid artifact descriptor")
	}
	if _, err := hex.DecodeString(d.digest); err != nil {
		return errors.New("invalid artifact digest")
	}
	return nil
}

func (d ArtifactDescriptor) ID() ArtifactID             { return d.id }
func (d ArtifactDescriptor) ThreadID() domain.ThreadID  { return d.thread }
func (d ArtifactDescriptor) InvocationID() InvocationID { return d.invocation }
func (d ArtifactDescriptor) Stream() ArtifactStream     { return d.stream }
func (d ArtifactDescriptor) SavedBytes() int64          { return d.saved }
func (d ArtifactDescriptor) ObservedBytes() int64       { return d.observed }
func (d ArtifactDescriptor) Digest() string             { return d.digest }
func (d ArtifactDescriptor) Complete() bool             { return d.complete }

// ArtifactSaveStatus 区分保存完整性与命令退出状态。
type ArtifactSaveStatus string

const (
	ArtifactNotNeeded   ArtifactSaveStatus = "not_needed"
	ArtifactComplete    ArtifactSaveStatus = "complete"
	ArtifactPartial     ArtifactSaveStatus = "partial"
	ArtifactUnavailable ArtifactSaveStatus = "unavailable"
)

// ArtifactSaving 保存一个流的冻结存储结果。
type ArtifactSaving struct {
	status     ArtifactSaveStatus
	reason     string
	descriptor ArtifactDescriptor
}

// NewArtifactSaving 校验保存状态与可用引用的一致性。
func NewArtifactSaving(status ArtifactSaveStatus, reason string, descriptor ArtifactDescriptor) (ArtifactSaving, error) {
	s := ArtifactSaving{status, reason, descriptor}
	return s, s.Validate()
}

// Validate 拒绝错误引用、未知原因和不一致完整性。
func (s ArtifactSaving) Validate() error {
	switch s.status {
	case ArtifactNotNeeded:
		if s.reason == "" && s.descriptor == (ArtifactDescriptor{}) {
			return nil
		}
	case ArtifactComplete:
		if s.reason == "" && s.descriptor.Validate() == nil && s.descriptor.complete {
			return nil
		}
	case ArtifactPartial, ArtifactUnavailable:
		switch s.reason {
		case "quota_exceeded", "stream_limit", "write_failed", "sync_failed", "publish_failed", "output_incomplete", "storage_unavailable":
			if s.status == ArtifactUnavailable && s.descriptor == (ArtifactDescriptor{}) {
				return nil
			}
			if s.status == ArtifactPartial && s.descriptor.Validate() == nil && !s.descriptor.complete {
				return nil
			}
		}
	}
	return errors.New("invalid artifact saving state")
}
func (s ArtifactSaving) Status() ArtifactSaveStatus { return s.status }
func (s ArtifactSaving) Reason() string             { return s.reason }
func (s ArtifactSaving) Descriptor() (ArtifactDescriptor, bool) {
	return s.descriptor, s.status == ArtifactComplete || s.status == ArtifactPartial
}

// ReadArtifactInput 是按原始字节定位的不可变范围输入。
type ReadArtifactInput struct {
	id     ArtifactID
	offset uint64
	limit  uint64
}

// NewReadArtifactInput 构造明确范围；零 limit 不代表默认值。
func NewReadArtifactInput(id ArtifactID, offset, limit uint64) (ReadArtifactInput, error) {
	input := ReadArtifactInput{id, offset, limit}
	return input, input.Validate()
}

// DecodeReadArtifactInput 严格解码并仅对缺失字段设置默认值。
func DecodeReadArtifactInput(data []byte) (ReadArtifactInput, error) {
	if len(data) == 0 || len(data) > 1024 {
		return ReadArtifactInput{}, errors.New("invalid ReadArtifact input size")
	}
	wire := struct {
		ID     ArtifactID `json:"artifact_id"`
		Offset uint64     `json:"offset"`
		Limit  uint64     `json:"limit"`
	}{Limit: ArtifactPageBytes}
	if err := codec.DecodeObject(data, &wire, "artifact_id"); err != nil {
		return ReadArtifactInput{}, err
	}
	return NewReadArtifactInput(wire.ID, wire.Offset, wire.Limit)
}
func (input ReadArtifactInput) ID() ArtifactID { return input.id }
func (input ReadArtifactInput) Offset() uint64 { return input.offset }
func (input ReadArtifactInput) Limit() uint64  { return input.limit }

// Validate 只校验输入格式，具体文件范围由执行时验证。
func (input ReadArtifactInput) Validate() error {
	if !input.id.Valid() || input.limit < 1 || input.limit > ArtifactPageBytes {
		return errors.New("invalid ReadArtifact input")
	}
	return nil
}
