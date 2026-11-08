package domain

import (
	"easycode/internal/codec"
	"encoding/json"
)

// HookPatchMetadata 是成功修改的本地语义投影，不携带正文或 artifact。
type HookPatchMetadata struct {
	FilePath         string `json:"file_path"`
	ReplacementCount int    `json:"replacement_count"`
	AddedLines       int    `json:"added_lines"`
	RemovedLines     int    `json:"removed_lines"`
}

// HookWriteMetadata 是已持久化写入结果的有限投影。
type HookWriteMetadata struct {
	FilePath           string `json:"file_path"`
	Operation          string `json:"operation"`
	Bytes              int64  `json:"bytes"`
	Lines              int    `json:"lines"`
	CreatedDirectories int    `json:"created_directories"`
}

// HookExecMetadata 只描述进程统计，不传递 stdout/stderr 或宿主标识。
type HookExecMetadata struct {
	Workdir         string `json:"workdir"`
	ExitKnown       bool   `json:"exit_known"`
	ExitCode        int    `json:"exit_code"`
	StdoutBytes     int64  `json:"stdout_bytes"`
	StderrBytes     int64  `json:"stderr_bytes"`
	OutputTruncated bool   `json:"output_truncated"`
	TimedOut        bool   `json:"timed_out"`
	Cancelled       bool   `json:"cancelled"`
}

// HookToolResponse 是仅允许三类真实成功结果的封闭联合。
type HookToolResponse struct {
	capability HookCapability
	patch      HookPatchMetadata
	write      HookWriteMetadata
	exec       HookExecMetadata
}

func NewHookPatchResponse(m HookPatchMetadata) (HookToolResponse, error) {
	v := HookToolResponse{capability: HookPatch, patch: m}
	return v, v.Validate()
}
func NewHookWriteResponse(m HookWriteMetadata) (HookToolResponse, error) {
	v := HookToolResponse{capability: HookWrite, write: m}
	return v, v.Validate()
}
func NewHookExecResponse(m HookExecMetadata) (HookToolResponse, error) {
	v := HookToolResponse{capability: HookExec, exec: m}
	return v, v.Validate()
}
func (v HookToolResponse) Capability() HookCapability { return v.capability }
func (v HookToolResponse) Validate() error {
	switch v.capability {
	case HookPatch:
		m := v.patch
		if !hookRelativePath(m.FilePath, false) || m.ReplacementCount < 1 || m.AddedLines < 0 || m.RemovedLines < 0 {
			return HookConfigInvalid
		}
	case HookWrite:
		m := v.write
		if !hookRelativePath(m.FilePath, false) || (m.Operation != "create" && m.Operation != "overwrite") || m.Bytes < 0 || m.Lines < 0 || int64(m.Lines) > m.Bytes || (m.Bytes == 0) != (m.Lines == 0) || m.CreatedDirectories < 0 || m.Operation == "overwrite" && m.CreatedDirectories != 0 {
			return HookConfigInvalid
		}
	case HookExec:
		m := v.exec
		if !hookRelativePath(m.Workdir, true) || !m.ExitKnown || m.ExitCode < 0 || m.ExitCode > 255 || m.StdoutBytes < 0 || m.StderrBytes < 0 || m.TimedOut || m.Cancelled {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	return nil
}
func (v HookToolResponse) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	switch v.capability {
	case HookPatch:
		return codec.MarshalStable(struct {
			Capability HookCapability    `json:"capability"`
			Status     string            `json:"status"`
			Code       string            `json:"code"`
			Metadata   HookPatchMetadata `json:"metadata"`
		}{v.capability, "success", "ok", v.patch})
	case HookWrite:
		return codec.MarshalStable(struct {
			Capability HookCapability    `json:"capability"`
			Status     string            `json:"status"`
			Code       string            `json:"code"`
			Metadata   HookWriteMetadata `json:"metadata"`
		}{v.capability, "success", "ok", v.write})
	case HookExec:
		return codec.MarshalStable(struct {
			Capability HookCapability   `json:"capability"`
			Status     string           `json:"status"`
			Code       string           `json:"code"`
			Metadata   HookExecMetadata `json:"metadata"`
		}{v.capability, "success", "ok", v.exec})
	default:
		return nil, HookConfigInvalid
	}
}
func DecodeHookToolResponse(b []byte) (HookToolResponse, error) {
	members, err := codec.ObjectMembers(b)
	if err != nil || len(members) != 4 {
		return HookToolResponse{}, HookConfigInvalid
	}
	var header struct {
		Capability HookCapability  `json:"capability"`
		Status     string          `json:"status"`
		Code       string          `json:"code"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if codec.UnmarshalRecord(b, &header) != nil || header.Status != "success" || header.Code != "ok" {
		return HookToolResponse{}, HookConfigInvalid
	}
	switch header.Capability {
	case HookPatch:
		var m HookPatchMetadata
		if codec.UnmarshalRecord(header.Metadata, &m) != nil {
			return HookToolResponse{}, HookConfigInvalid
		}
		return NewHookPatchResponse(m)
	case HookWrite:
		var m HookWriteMetadata
		if codec.UnmarshalRecord(header.Metadata, &m) != nil {
			return HookToolResponse{}, HookConfigInvalid
		}
		return NewHookWriteResponse(m)
	case HookExec:
		var m HookExecMetadata
		if codec.UnmarshalRecord(header.Metadata, &m) != nil {
			return HookToolResponse{}, HookConfigInvalid
		}
		return NewHookExecResponse(m)
	default:
		return HookToolResponse{}, HookConfigInvalid
	}
}
