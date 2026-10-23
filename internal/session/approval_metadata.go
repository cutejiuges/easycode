package session

import (
	"errors"

	"easycode/internal/tool"
)

// ApprovalDecisionSource 区分用户、宿主策略和已有授权，不携带用户或进程身份。
type ApprovalDecisionSource string

const (
	ApprovalSourceInteractive  ApprovalDecisionSource = "interactive"
	ApprovalSourcePolicy       ApprovalDecisionSource = "policy"
	ApprovalSourceSessionGrant ApprovalDecisionSource = "session_grant"
	ApprovalSourceHeadless     ApprovalDecisionSource = "headless"
	ApprovalSourceInterruption ApprovalDecisionSource = "interruption"
)

func (source ApprovalDecisionSource) Valid() bool {
	switch source {
	case ApprovalSourceInteractive, ApprovalSourcePolicy, ApprovalSourceSessionGrant, ApprovalSourceHeadless, ApprovalSourceInterruption:
		return true
	default:
		return false
	}
}

// ApprovalMetadataPayload 只保存可校验的相对目标与时间预算，不重复命令、diff或环境正文。
type ApprovalMetadataPayload struct {
	Capability    tool.CapabilityID `json:"capability"`
	RelativePath  string            `json:"relative_path"`
	TimeoutMillis int               `json:"timeout_ms"`
}

// NewApprovalMetadata 从唯一当前invocation提取安全展示元数据。
func NewApprovalMetadata(invocation tool.Invocation) (ApprovalMetadataPayload, error) {
	if invocation.Validate() != nil {
		return ApprovalMetadataPayload{}, errors.New("approval invocation is invalid")
	}
	metadata := ApprovalMetadataPayload{Capability: invocation.Capability()}
	if exec, ok := invocation.Exec(); ok {
		metadata.RelativePath, metadata.TimeoutMillis = exec.Input().Workdir(), exec.Input().TimeoutMillis()
	} else if patch, ok := invocation.Patch(); ok {
		metadata.RelativePath = patch.Input().FilePath()
	}
	if err := metadata.Validate(); err != nil {
		return ApprovalMetadataPayload{}, err
	}
	return metadata, nil
}

func (metadata ApprovalMetadataPayload) Validate() error {
	switch metadata.Capability {
	case tool.CapabilityPatch:
		if _, err := tool.NewPatchResultMetadata(metadata.RelativePath, 1, 0, 0); err != nil || metadata.TimeoutMillis != 0 {
			return errors.New("patch approval metadata is invalid")
		}
	case tool.CapabilityExec:
		if metadata.TimeoutMillis < tool.MinExecTimeoutMillis || metadata.TimeoutMillis > tool.MaxExecTimeoutMillis {
			return errors.New("exec approval metadata is invalid")
		}
		if _, err := tool.NewExecInput("validation", metadata.RelativePath, metadata.TimeoutMillis); err != nil {
			return errors.New("exec approval metadata is invalid")
		}
	default:
		return errors.New("approval metadata capability is invalid")
	}
	return nil
}
