package tool

import "easycode/internal/domain"

// NewWriteApprovalRequest 只从完整冻结的写入候选构造批准请求。
func NewWriteApprovalRequest(id ApprovalRequestID, session domain.SessionID, thread domain.ThreadID, turn domain.TurnID, c WriteCandidate) (ApprovalRequest, error) {
	if err := c.Validate(); err != nil {
		return ApprovalRequest{}, err
	}
	reason := "Create a workspace file and its listed parent directories."
	if c.Operation() == WriteOverwrite {
		reason = "Overwrite an existing workspace file after a complete Read."
	}
	r := ApprovalRequest{requestID: id, sessionID: session, threadID: thread, turnID: turn, invocationID: c.InvocationID(), providerCallID: c.ProviderCallID(), capability: CapabilityWrite, reason: reason, relativePath: c.RelativePath(), fingerprint: c.Fingerprint(), diff: c.Preview(), decisions: []ApprovalDecision{ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny}, writeMetadata: c.ResultMetadata(), directories: c.Directories()}
	return r, r.Validate()
}

func (r ApprovalRequest) WriteMetadata() WriteResultMetadata { return r.writeMetadata }
func (r ApprovalRequest) Directories() []string              { return append([]string(nil), r.directories...) }
