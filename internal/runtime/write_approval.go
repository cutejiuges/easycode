package runtime

import (
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

func approvalPresentation(request tool.ApprovalRequest) (protocol.ApprovalPresentation, error) {
	if request.Capability() == tool.CapabilityPatch {
		return protocol.NewPatchApprovalPresentation(request.Diff())
	}
	m := request.WriteMetadata()
	if m.Operation() == tool.WriteCreate {
		return protocol.NewWriteCreateApprovalPresentation(request.Diff(), m.Bytes(), m.Lines(), request.Directories())
	}
	return protocol.NewWriteOverwriteApprovalPresentation(request.Diff(), m.Bytes(), m.Lines())
}
