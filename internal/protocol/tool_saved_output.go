package protocol

import (
	"easycode/internal/domain"
	"errors"
)

// ToolOutputReference 是授权对象的纯展示投影，不包含宿主路径或文件句柄。
type ToolOutputReference struct {
	ID            string           `json:"artifact_id"`
	ThreadID      domain.ThreadID  `json:"thread_id"`
	InvocationID  string           `json:"invocation_id"`
	Source        ToolOutputSource `json:"source"`
	SavedBytes    int64            `json:"saved_bytes"`
	ObservedBytes int64            `json:"observed_bytes"`
	SHA256        string           `json:"sha256"`
	Complete      bool             `json:"complete"`
}

func (r ToolOutputReference) Validate() error {
	if !fixedHex(r.ID, 32) || !fixedHex(r.SHA256, 64) || !r.ThreadID.Valid() || !validUUIDv7Text(r.InvocationID) || (r.Source != ToolStdout && r.Source != ToolStderr && r.Source != ToolSearchResult) || r.SavedBytes < 1 || r.SavedBytes > 16<<20 || r.ObservedBytes < r.SavedBytes || (r.Complete && r.SavedBytes != r.ObservedBytes) {
		return errors.New("invalid saved output reference")
	}
	return nil
}
func fixedHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ToolOutputInfo 将冻结保存状态与可选授权引用组成唯一当前联合。
type ToolOutputInfo struct {
	Summary   ToolOutputSummary    `json:"summary"`
	Reference *ToolOutputReference `json:"reference,omitempty"`
}

func (v ToolOutputInfo) Validate() error {
	if v.Summary.Validate() != nil {
		return errors.New("invalid saved output summary")
	}
	if !v.Summary.Readable {
		if v.Reference != nil {
			return errors.New("unexpected saved output reference")
		}
		return nil
	}
	r := v.Reference
	if r == nil || r.Validate() != nil || r.SavedBytes != v.Summary.SavedBytes || r.ObservedBytes != v.Summary.ObservedBytes || r.Complete != v.Summary.Complete {
		return errors.New("saved output reference mismatch")
	}
	return nil
}
func validateTerminalSaving(p ToolTerminalPayload) error {
	for _, entry := range []struct {
		source  ToolOutputSource
		info    ToolOutputInfo
		enabled bool
	}{
		{ToolStdout, p.Stdout, p.Tool == ToolShell}, {ToolStderr, p.Stderr, p.Tool == ToolShell}, {ToolSearchResult, p.Search, p.Tool == ToolGlob || p.Tool == ToolGrep},
	} {
		if entry.info.Validate() != nil || (!entry.enabled && entry.info != (ToolOutputInfo{})) || (entry.enabled && entry.info.Summary.Status == "") {
			return errors.New("invalid terminal saving union")
		}
		if r := entry.info.Reference; r != nil && (r.Source != entry.source || r.InvocationID != p.InvocationID) {
			return errors.New("terminal output identity mismatch")
		}
	}
	return nil
}

func NewToolOutputReference(reference ToolOutputReference) (ToolOutputReference, error) {
	return reference, reference.Validate()
}
func DecodeToolOutputReference(data []byte) (ToolOutputReference, error) {
	return decodeTranscriptValue[ToolOutputReference](data)
}
func NewToolOutputInfo(info ToolOutputInfo) (ToolOutputInfo, error) {
	if info.Reference != nil {
		copy := *info.Reference
		info.Reference = &copy
	}
	return info, info.Validate()
}
func DecodeToolOutputInfo(data []byte) (ToolOutputInfo, error) {
	return decodeTranscriptValue[ToolOutputInfo](data)
}
