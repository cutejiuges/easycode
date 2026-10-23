package tool

import "errors"

// ReadObservationKind 区分经过完整读取的证据与明确不可用状态。
type ReadObservationKind string

const (
	ReadObservationUnavailable ReadObservationKind = "unavailable"
	ReadObservationComplete    ReadObservationKind = "complete"
)

// ReadObservation 只在当前进程流转，不含正文或可序列化的会话字段。
type ReadObservation struct {
	kind           ReadObservationKind
	invocationID   InvocationID
	providerCallID ProviderCallID
	relativePath   string
	evidence       FileBaseEvidence
}

// UnavailableReadObservation 显式构造不能授权覆盖的观察。
func UnavailableReadObservation() ReadObservation {
	return ReadObservation{kind: ReadObservationUnavailable}
}

// NewCompleteReadObservation 绑定已验证的完整结果和实际文件证据。
func NewCompleteReadObservation(result InvocationResult, evidence FileBaseEvidence) (ReadObservation, error) {
	m := result.ReadMetadata()
	if result.Validate() != nil || result.Capability() != CapabilityRead || result.Status() != ResultSuccess || m.RequestedOffset() != 1 || !m.ReachedEOF() || m.LongLineTruncated() || m.OutputTruncated() || validateWritePath(m.RelativePath()) != nil || evidence.Validate() != nil {
		return ReadObservation{}, errors.New("Read observation requires a complete result and file evidence")
	}
	o := ReadObservation{kind: ReadObservationComplete, invocationID: result.InvocationID(), providerCallID: result.ProviderCallID(), relativePath: m.RelativePath(), evidence: evidence}
	return o, o.Validate()
}

func (o ReadObservation) Kind() ReadObservationKind  { return o.kind }
func (o ReadObservation) RelativePath() string       { return o.relativePath }
func (o ReadObservation) InvocationID() InvocationID { return o.invocationID }
func (o ReadObservation) Evidence() FileBaseEvidence { return o.evidence }

// Validate 拒绝零值以及不可用分支中的伪造证据。
func (o ReadObservation) Validate() error {
	switch o.kind {
	case ReadObservationUnavailable:
		if o.invocationID == "" && o.providerCallID == "" && o.relativePath == "" && o.evidence == (FileBaseEvidence{}) {
			return nil
		}
	case ReadObservationComplete:
		if o.invocationID.Valid() && o.providerCallID.Valid() && validateWritePath(o.relativePath) == nil && o.evidence.Validate() == nil {
			return nil
		}
	}
	return errors.New("invalid Read observation")
}

// ReadExecution 是 Read 的唯一返回契约，冻结模型结果与瞬态观察分别持有。
type ReadExecution struct {
	result      InvocationResult
	observation ReadObservation
}

// NewReadExecution 验证观察与实际返回结果的身份、路径及完整性一致。
func NewReadExecution(result InvocationResult, observation ReadObservation) (ReadExecution, error) {
	e := ReadExecution{result: result, observation: observation}
	return e, e.Validate()
}

func (e ReadExecution) Result() InvocationResult     { return e.result }
func (e ReadExecution) Observation() ReadObservation { return e.observation }

// Validate 拒绝结果与观察的混用，不执行磁盘读取。
func (e ReadExecution) Validate() error {
	if e.result.Validate() != nil || e.result.Capability() != CapabilityRead || e.observation.Validate() != nil {
		return errors.New("invalid Read execution")
	}
	if e.observation.kind == ReadObservationComplete {
		want, err := NewCompleteReadObservation(e.result, e.observation.evidence)
		if err != nil || want != e.observation {
			return errors.New("Read observation does not match result")
		}
	}
	return nil
}
