package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
)

const (
	// MaxApprovalFeedbackBytes 限制拒绝反馈的UTF-8字节数。
	MaxApprovalFeedbackBytes = 16 << 10
	// MaxApprovalDiffBytes 限制必须完整展示的批准diff。
	MaxApprovalDiffBytes   = 256 << 10
	maxApprovalReasonBytes = 1024
	maxPatchFileBytes      = 16 << 20
)

// ApprovalRequestID 标识一次进程内approval请求。
type ApprovalRequestID string

// GenerateApprovalRequestID 创建新的UUIDv7 approval请求标识。
func GenerateApprovalRequestID() (ApprovalRequestID, error) {
	id, err := GenerateInvocationID()
	return ApprovalRequestID(id), err
}

// ParseApprovalRequestID 校验canonical UUIDv7 approval请求标识。
func ParseApprovalRequestID(value string) (ApprovalRequestID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", errors.New("invalid approval request ID")
	}
	return ApprovalRequestID(value), nil
}

// Valid 判断approval请求标识是否合法。
func (id ApprovalRequestID) Valid() bool {
	_, err := parseUUIDv7(string(id))
	return err == nil
}

// CandidateFingerprint 是绑定typed input、base evidence与final bytes的SHA-256。
type CandidateFingerprint string

// ParseCandidateFingerprint 校验小写hex SHA-256 fingerprint。
func ParseCandidateFingerprint(value string) (CandidateFingerprint, error) {
	if !validSHA256Hex(value) {
		return "", errors.New("invalid candidate fingerprint")
	}
	return CandidateFingerprint(value), nil
}

// NewCandidateFingerprint 对给定canonical bytes计算fingerprint。
func NewCandidateFingerprint(canonical []byte) CandidateFingerprint {
	sum := sha256.Sum256(canonical)
	return CandidateFingerprint(hex.EncodeToString(sum[:]))
}

// Valid 判断candidate fingerprint是否合法。
func (fingerprint CandidateFingerprint) Valid() bool {
	return validSHA256Hex(string(fingerprint))
}

// ContentDigest 是文件内容的SHA-256摘要。
type ContentDigest string

// NewContentDigest 计算内容摘要。
func NewContentDigest(content []byte) ContentDigest {
	sum := sha256.Sum256(content)
	return ContentDigest(hex.EncodeToString(sum[:]))
}

// ParseContentDigest 校验内容摘要。
func ParseContentDigest(value string) (ContentDigest, error) {
	if !validSHA256Hex(value) {
		return "", errors.New("invalid content digest")
	}
	return ContentDigest(value), nil
}

// Valid 判断内容摘要是否合法。
func (digest ContentDigest) Valid() bool { return validSHA256Hex(string(digest)) }

// PatchBaseEvidence 保存与实际打开文件句柄绑定的基线证据。
type PatchBaseEvidence struct {
	contentDigest ContentDigest
	size          int64
	mode          uint32
	device        uint64
	inode         uint64
}

// NewPatchBaseEvidence 构造文件基线证据。
func NewPatchBaseEvidence(digest ContentDigest, size int64, mode uint32, device uint64, inode uint64) (PatchBaseEvidence, error) {
	evidence := PatchBaseEvidence{contentDigest: digest, size: size, mode: mode, device: device, inode: inode}
	if err := evidence.Validate(); err != nil {
		return PatchBaseEvidence{}, err
	}
	return evidence, nil
}

func (evidence PatchBaseEvidence) ContentDigest() ContentDigest { return evidence.contentDigest }
func (evidence PatchBaseEvidence) Size() int64                  { return evidence.size }
func (evidence PatchBaseEvidence) Mode() uint32                 { return evidence.mode }
func (evidence PatchBaseEvidence) Device() uint64               { return evidence.device }
func (evidence PatchBaseEvidence) Inode() uint64                { return evidence.inode }

// Validate 验证基线证据字段。
func (evidence PatchBaseEvidence) Validate() error {
	if !evidence.contentDigest.Valid() || evidence.size < 0 || evidence.size > maxPatchFileBytes || evidence.mode > 0o7777 || evidence.device == 0 || evidence.inode == 0 {
		return errors.New("patch base evidence is invalid")
	}
	return nil
}

// PatchCandidate 保存已验证且可在批准后重新核验的不可变候选。
type PatchCandidate struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	wireKind       ProviderWireKind
	input          PatchInput
	evidence       PatchBaseEvidence
	finalBytes     []byte
	diff           string
	fingerprint    CandidateFingerprint
	summary        PatchResultMetadata
}

// NewPatchCandidate 构造并深拷贝一个Patch候选。
func NewPatchCandidate(invocation PatchInvocation, evidence PatchBaseEvidence, finalBytes []byte, diff string, fingerprint CandidateFingerprint, summary PatchResultMetadata) (PatchCandidate, error) {
	candidate := PatchCandidate{
		invocationID: invocation.invocationID, providerCallID: invocation.providerCallID,
		wireKind: invocation.wireKind, input: invocation.input.Clone(), evidence: evidence, finalBytes: append([]byte(nil), finalBytes...),
		diff: diff, fingerprint: fingerprint, summary: summary,
	}
	if err := candidate.Validate(); err != nil {
		return PatchCandidate{}, err
	}
	return candidate, nil
}

func (candidate PatchCandidate) InvocationID() InvocationID     { return candidate.invocationID }
func (candidate PatchCandidate) ProviderCallID() ProviderCallID { return candidate.providerCallID }
func (candidate PatchCandidate) WireKind() ProviderWireKind     { return candidate.wireKind }
func (candidate PatchCandidate) Input() PatchInput              { return candidate.input.Clone() }
func (candidate PatchCandidate) Evidence() PatchBaseEvidence    { return candidate.evidence }
func (candidate PatchCandidate) FinalBytes() []byte {
	return append([]byte(nil), candidate.finalBytes...)
}
func (candidate PatchCandidate) Diff() string                        { return candidate.diff }
func (candidate PatchCandidate) Fingerprint() CandidateFingerprint   { return candidate.fingerprint }
func (candidate PatchCandidate) ResultMetadata() PatchResultMetadata { return candidate.summary }
func (candidate PatchCandidate) RelativePath() string                { return candidate.input.FilePath() }
func (candidate PatchCandidate) ReplacementCount() int               { return len(candidate.input.replacements) }

// Validate 验证候选身份、内容、展示预算及metadata绑定。
func (candidate PatchCandidate) Validate() error {
	if !candidate.invocationID.Valid() || !candidate.providerCallID.Valid() || !candidate.wireKind.Valid() || candidate.input.Validate() != nil || candidate.evidence.Validate() != nil || !candidate.fingerprint.Valid() {
		return errors.New("patch candidate identity or evidence is invalid")
	}
	if len(candidate.finalBytes) > maxPatchFileBytes || !utf8.Valid(candidate.finalBytes) || bytes.IndexByte(candidate.finalBytes, 0) >= 0 || len(candidate.diff) == 0 || len(candidate.diff) > MaxApprovalDiffBytes || !utf8.ValidString(candidate.diff) {
		return errors.New("patch candidate content or diff is invalid")
	}
	if candidate.summary.Validate() != nil || candidate.summary.relativePath != candidate.input.filePath || candidate.summary.replacementCount != len(candidate.input.replacements) {
		return errors.New("patch candidate metadata does not match input")
	}
	want, err := PatchFingerprint(candidate.input, candidate.evidence, candidate.finalBytes)
	if err != nil || candidate.fingerprint != want {
		return errors.New("patch candidate fingerprint does not match content")
	}
	return nil
}

// ApprovalDecision 是交互宿主可以提交的封闭决定。
type ApprovalDecision string

const (
	ApprovalAllowOnce    ApprovalDecision = "allow_once"
	ApprovalAllowSession ApprovalDecision = "allow_session"
	ApprovalDeny         ApprovalDecision = "deny"
	ApprovalAbort        ApprovalDecision = "abort"
)

// Valid 判断交互决定是否合法。
func (decision ApprovalDecision) Valid() bool {
	switch decision {
	case ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny, ApprovalAbort:
		return true
	default:
		return false
	}
}

// ApprovalOutcome 是durable approval事实支持的封闭结果。
type ApprovalOutcome string

const (
	ApprovalOutcomeAllowOnce        ApprovalOutcome = "allow_once"
	ApprovalOutcomeAllowSession     ApprovalOutcome = "allow_session"
	ApprovalOutcomeAllowFromSession ApprovalOutcome = "allow_from_session"
	ApprovalOutcomeDeny             ApprovalOutcome = "deny"
	ApprovalOutcomeAbort            ApprovalOutcome = "abort"
)

// Valid 判断durable approval结果是否合法。
func (outcome ApprovalOutcome) Valid() bool {
	switch outcome {
	case ApprovalOutcomeAllowOnce, ApprovalOutcomeAllowSession, ApprovalOutcomeAllowFromSession, ApprovalOutcomeDeny, ApprovalOutcomeAbort:
		return true
	default:
		return false
	}
}

// ApprovalRequest 是Runtime发布给交互宿主的不可变批准请求。
type ApprovalRequest struct {
	requestID      ApprovalRequestID
	sessionID      domain.SessionID
	threadID       domain.ThreadID
	turnID         domain.TurnID
	invocationID   InvocationID
	providerCallID ProviderCallID
	capability     CapabilityID
	reason         string
	relativePath   string
	fingerprint    CandidateFingerprint
	diff           string
	decisions      []ApprovalDecision
}

// NewApprovalRequest 构造并深拷贝一个批准请求。
func NewApprovalRequest(requestID ApprovalRequestID, sessionID domain.SessionID, threadID domain.ThreadID, turnID domain.TurnID, candidate PatchCandidate, reason string, decisions []ApprovalDecision) (ApprovalRequest, error) {
	if err := candidate.Validate(); err != nil {
		return ApprovalRequest{}, err
	}
	request := ApprovalRequest{
		requestID: requestID, sessionID: sessionID, threadID: threadID, turnID: turnID,
		invocationID: candidate.invocationID, providerCallID: candidate.providerCallID,
		capability: CapabilityPatch, reason: reason, relativePath: candidate.RelativePath(),
		fingerprint: candidate.fingerprint, diff: candidate.diff, decisions: append([]ApprovalDecision(nil), decisions...),
	}
	if err := request.Validate(); err != nil {
		return ApprovalRequest{}, err
	}
	return request, nil
}

func (request ApprovalRequest) RequestID() ApprovalRequestID      { return request.requestID }
func (request ApprovalRequest) SessionID() domain.SessionID       { return request.sessionID }
func (request ApprovalRequest) ThreadID() domain.ThreadID         { return request.threadID }
func (request ApprovalRequest) TurnID() domain.TurnID             { return request.turnID }
func (request ApprovalRequest) InvocationID() InvocationID        { return request.invocationID }
func (request ApprovalRequest) ProviderCallID() ProviderCallID    { return request.providerCallID }
func (request ApprovalRequest) Capability() CapabilityID          { return request.capability }
func (request ApprovalRequest) Reason() string                    { return request.reason }
func (request ApprovalRequest) RelativePath() string              { return request.relativePath }
func (request ApprovalRequest) Fingerprint() CandidateFingerprint { return request.fingerprint }
func (request ApprovalRequest) Diff() string                      { return request.diff }
func (request ApprovalRequest) Decisions() []ApprovalDecision {
	return append([]ApprovalDecision(nil), request.decisions...)
}

// Validate 验证请求identity、显示数据和固定决定集合。
func (request ApprovalRequest) Validate() error {
	if !request.requestID.Valid() || !request.sessionID.Valid() || !request.threadID.Valid() || !request.turnID.Valid() || !request.invocationID.Valid() || !request.providerCallID.Valid() || request.capability != CapabilityPatch || !request.fingerprint.Valid() {
		return errors.New("approval request identity is invalid")
	}
	if request.relativePath == "" || validateSearchPath(request.relativePath) != nil || !validSafeText(request.reason, maxApprovalReasonBytes) || request.diff == "" || len(request.diff) > MaxApprovalDiffBytes || !utf8.ValidString(request.diff) {
		return errors.New("approval request presentation is invalid")
	}
	want := []ApprovalDecision{ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny}
	if len(request.decisions) != len(want) {
		return errors.New("approval request decisions are invalid")
	}
	for index := range want {
		if request.decisions[index] != want[index] {
			return errors.New("approval request decisions are invalid")
		}
	}
	return nil
}

// ApprovalResponse 是宿主对一个活动请求提交的关联响应。
type ApprovalResponse struct {
	requestID    ApprovalRequestID
	turnID       domain.TurnID
	invocationID InvocationID
	fingerprint  CandidateFingerprint
	decision     ApprovalDecision
	feedback     string
}

// NewApprovalResponse 构造approval响应。
func NewApprovalResponse(requestID ApprovalRequestID, turnID domain.TurnID, invocationID InvocationID, fingerprint CandidateFingerprint, decision ApprovalDecision, feedback string) (ApprovalResponse, error) {
	response := ApprovalResponse{requestID: requestID, turnID: turnID, invocationID: invocationID, fingerprint: fingerprint, decision: decision, feedback: strings.TrimSpace(feedback)}
	if err := response.Validate(); err != nil {
		return ApprovalResponse{}, err
	}
	return response, nil
}

func (response ApprovalResponse) RequestID() ApprovalRequestID      { return response.requestID }
func (response ApprovalResponse) TurnID() domain.TurnID             { return response.turnID }
func (response ApprovalResponse) InvocationID() InvocationID        { return response.invocationID }
func (response ApprovalResponse) Fingerprint() CandidateFingerprint { return response.fingerprint }
func (response ApprovalResponse) Decision() ApprovalDecision        { return response.decision }
func (response ApprovalResponse) Feedback() string                  { return response.feedback }

// Validate 验证response关联字段和反馈边界。
func (response ApprovalResponse) Validate() error {
	if !response.requestID.Valid() || !response.turnID.Valid() || !response.invocationID.Valid() || !response.fingerprint.Valid() || !response.decision.Valid() {
		return errors.New("approval response identity or decision is invalid")
	}
	if response.feedback != "" && response.decision != ApprovalDeny {
		return errors.New("approval feedback is only valid for deny")
	}
	if !utf8.ValidString(response.feedback) || len(response.feedback) > MaxApprovalFeedbackBytes {
		return errors.New("approval feedback is invalid")
	}
	return nil
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validSafeText(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return false
		}
	}
	return true
}
