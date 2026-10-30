package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// validateArtifactHistory 按 durable 顺序验证引用，不能由未来结果授权较早读取。
// 这里只检查事实关联，不访问对象；缺失或篡改的文件不能改变原生恢复。
func validateArtifactHistory(thread domain.ThreadID, records []Record) error {
	references, err := tool.NewArtifactReferences(thread, nil)
	if err != nil {
		return replayCorrupt("artifact reference thread is invalid")
	}
	for _, record := range records {
		if record.EventKind != EventToolCallResult {
			continue
		}
		payload, err := DecodeToolCallResultPayload(record)
		if err != nil {
			return replayCorrupt("artifact result payload is invalid")
		}
		switch payload.Capability {
		case tool.CapabilityExec, tool.CapabilityGlob, tool.CapabilityGrep, tool.CapabilityReadArtifact:
		default:
			continue
		}
		invocation, err := validationInvocation(payload)
		if err != nil {
			return replayCorrupt("artifact result invocation is invalid")
		}
		result, err := payload.Domain(invocation)
		if err != nil {
			return replayCorrupt("artifact result is invalid")
		}
		if payload.Capability != tool.CapabilityReadArtifact {
			if references.ValidatePending(result) != nil || references.AuthorizeCommitted(result) != nil {
				return replayCorrupt("artifact references conflict")
			}
			continue
		}
		if page, ok := result.ArtifactPage(); ok {
			d, exists := references.Lookup(page.ID())
			if !exists || uint64(d.SavedBytes()) != page.SavedBytes() || d.Complete() != page.SourceComplete() {
				return replayCorrupt("artifact page has no matching committed reference")
			}
		}
	}
	return nil
}
