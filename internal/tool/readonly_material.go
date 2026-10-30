package tool

import "errors"

// ReadonlyMaterial 是 worker 交给顺序提交 owner 的封闭材料，不携带可执行回调。
type ReadonlyMaterial struct {
	capability  CapabilityID
	result      InvocationResult
	observation ReadObservation
	artifact    ReadArtifactExecution
	search      SearchExecution
}

// NewReadMaterial 保留完整读取证据，在预算拒绝时统一失效。
func NewReadMaterial(execution ReadExecution) (ReadonlyMaterial, error) {
	if err := execution.Validate(); err != nil {
		return ReadonlyMaterial{}, err
	}
	return ReadonlyMaterial{capability: CapabilityRead, result: execution.Result(), observation: execution.Observation()}, nil
}

// NewArtifactMaterial 接收范围窗口或明确失败，不提前生成最终预览。
func NewArtifactMaterial(execution ReadArtifactExecution) (ReadonlyMaterial, error) {
	if execution.failed {
		if execution.failure.Validate() != nil {
			return ReadonlyMaterial{}, errors.New("invalid artifact failure")
		}
	} else if execution.invocation.Validate() != nil || execution.window.descriptor.Validate() != nil {
		return ReadonlyMaterial{}, errors.New("invalid artifact material")
	}
	return ReadonlyMaterial{capability: CapabilityReadArtifact, artifact: execution}, nil
}

// NewReadonlyResultMaterial 只接收明确失败；成功结果必须经过各能力材料契约。
func NewReadonlyResultMaterial(result InvocationResult) (ReadonlyMaterial, error) {
	if result.Validate() != nil || result.Status() == ResultSuccess || result.Capability().Exclusive() {
		return ReadonlyMaterial{}, errors.New("invalid readonly result material")
	}
	return ReadonlyMaterial{capability: result.Capability(), result: result, observation: UnavailableReadObservation()}, nil
}

// Finalize 只由顺序 owner 调用；返回不可用于重复执行的冻结结果。
func (m ReadonlyMaterial) Finalize(allowance int, saving ArtifactSaving) (InvocationResult, ReadObservation, error) {
	if m.capability == CapabilityGlob || m.capability == CapabilityGrep {
		if m.result.invocationID == "" {
			r, err := m.search.Finalize(allowance, saving)
			return r, UnavailableReadObservation(), err
		}
	}
	if m.capability == CapabilityReadArtifact && m.result.invocationID == "" {
		r, err := m.artifact.Finalize(allowance)
		return r, UnavailableReadObservation(), err
	}
	if m.capability == CapabilityRead {
		execution, err := NewReadExecution(m.result, m.observation)
		if err != nil {
			return InvocationResult{}, UnavailableReadObservation(), err
		}
		execution, err = execution.Finalize(allowance)
		return execution.Result(), execution.Observation(), err
	}
	r, err := FinalizeResult(m.result, allowance, BudgetPreserved)
	return r, UnavailableReadObservation(), err
}

// NewSearchMaterial 保存 worker 的有界候选，不持有存储资源。
func NewSearchMaterial(execution SearchExecution) (ReadonlyMaterial, error) {
	if execution.result.Validate() != nil {
		return ReadonlyMaterial{}, errors.New("invalid search material")
	}
	return ReadonlyMaterial{capability: execution.result.capability, search: execution}, nil
}

// SearchSavingNeeded 让保存 owner 在最终渲染前决定是否发布所选原文。
func (m ReadonlyMaterial) SearchSavingNeeded(allowance int) bool {
	return m.search.NeedsSaving(allowance)
}

// SearchComplete 表示选中结果已完成采集，不等同于查询空间覆盖完整。
func (m ReadonlyMaterial) SearchComplete() bool { return m.search.result.status == ResultSuccess }
