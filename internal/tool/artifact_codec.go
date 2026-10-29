package tool

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"encoding/json"
	"errors"
)

type artifactSavingWire struct {
	Status     ArtifactSaveStatus `json:"status"`
	Reason     string             `json:"reason"`
	Descriptor json.RawMessage    `json:"descriptor"`
}

// MarshalJSON 仅序列化已验证保存状态。
func (s ArtifactSaving) MarshalJSON() ([]byte, error) { return EncodeArtifactSaving(s) }

// UnmarshalJSON 使用唯一当前解码器恢复保存状态。
func (s *ArtifactSaving) UnmarshalJSON(data []byte) error {
	v, err := DecodeArtifactSaving(data)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// EncodeArtifactSaving 编码唯一当前保存状态；无引用显式编码 null。
func EncodeArtifactSaving(s ArtifactSaving) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	w := artifactSavingWire{Status: s.status, Reason: s.reason, Descriptor: json.RawMessage("null")}
	if d, ok := s.Descriptor(); ok {
		b, err := EncodeArtifactDescriptor(d)
		if err != nil {
			return nil, err
		}
		w.Descriptor = b
	}
	return codec.MarshalStable(w)
}

// DecodeArtifactSaving 验证标签并调用匹配的描述符解码器。
func DecodeArtifactSaving(data []byte) (ArtifactSaving, error) {
	var w artifactSavingWire
	if err := codec.UnmarshalRecord(data, &w); err != nil {
		return ArtifactSaving{}, err
	}
	if len(w.Descriptor) == 0 {
		return ArtifactSaving{}, errors.New("artifact descriptor field is required")
	}
	var d ArtifactDescriptor
	if string(w.Descriptor) != "null" {
		var err error
		d, err = DecodeArtifactDescriptor(w.Descriptor)
		if err != nil {
			return ArtifactSaving{}, err
		}
	}
	return NewArtifactSaving(w.Status, w.Reason, d)
}

type artifactPageWire struct {
	ID       ArtifactID `json:"artifact_id"`
	Offset   uint64     `json:"offset"`
	Next     uint64     `json:"next_offset"`
	Stored   uint64     `json:"saved_bytes"`
	Complete bool       `json:"source_complete"`
	Text     string     `json:"text"`
}

// MarshalJSON 序列化有界且已校验的冻结页。
func (p ArtifactPage) MarshalJSON() ([]byte, error) { return EncodeArtifactPage(p) }

// UnmarshalJSON 严格恢复页，不访问文件。
func (p *ArtifactPage) UnmarshalJSON(data []byte) error {
	v, err := DecodeArtifactPage(data)
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// EncodeArtifactPage 编码冻结页，不重新读取原始内容。
func EncodeArtifactPage(p ArtifactPage) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(artifactPageWire{p.id, p.offset, p.next, p.stored, p.complete, p.text})
}

// DecodeArtifactPage 严格恢复冻结范围及正文。
func DecodeArtifactPage(data []byte) (ArtifactPage, error) {
	if len(data) > 2*MaxModelPreviewBytes {
		return ArtifactPage{}, errors.New("artifact page record is too large")
	}
	var w artifactPageWire
	if err := codec.UnmarshalRecord(data, &w); err != nil {
		return ArtifactPage{}, err
	}
	return NewArtifactPage(w.ID, w.Offset, w.Next, w.Stored, w.Complete, w.Text)
}

type artifactDescriptorWire struct {
	ID         ArtifactID      `json:"id"`
	Thread     domain.ThreadID `json:"thread_id"`
	Invocation InvocationID    `json:"invocation_id"`
	Stream     ArtifactStream  `json:"stream"`
	Saved      int64           `json:"saved_bytes"`
	Observed   int64           `json:"observed_bytes"`
	Digest     string          `json:"digest"`
	Complete   bool            `json:"complete"`
}

// EncodeArtifactDescriptor 验证后确定性编码，不检查文件存在性。
func EncodeArtifactDescriptor(d ArtifactDescriptor) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(artifactDescriptorWire{d.id, d.thread, d.invocation, d.stream, d.saved, d.observed, d.digest, d.complete})
}

// DecodeArtifactDescriptor 拒绝旧形状、缺字段和未知扩展。
func DecodeArtifactDescriptor(data []byte) (ArtifactDescriptor, error) {
	var w artifactDescriptorWire
	if err := codec.UnmarshalRecord(data, &w); err != nil {
		return ArtifactDescriptor{}, err
	}
	return NewArtifactDescriptor(w.ID, w.Thread, w.Invocation, w.Stream, w.Saved, w.Observed, w.Digest, w.Complete)
}
