package provider

import "easycode/internal/domain"

// OptionalUint 区分 Provider 明确报告的零值和缺失字段。
type OptionalUint struct {
	Value uint64
	Known bool
}

// Metric 把原生数字转换为领域观测值。
func (value OptionalUint) Metric() domain.UsageMetric {
	if !value.Known {
		return domain.UnknownUsageMetric()
	}
	return domain.KnownUsageMetric(value.Value)
}

// OptionalUintCommit 保存两家原生提交使用的可选数字形状。
type OptionalUintCommit struct {
	Known bool    `json:"known"`
	Value *uint64 `json:"value,omitempty"`
}

// Commit 冻结原生数字及其已知状态。
func (value OptionalUint) Commit() OptionalUintCommit {
	if !value.Known {
		return OptionalUintCommit{}
	}
	number := value.Value
	return OptionalUintCommit{Known: true, Value: &number}
}

// OptionalUint 从适配器已验证的提交字段恢复数字。
func (commit OptionalUintCommit) OptionalUint() OptionalUint {
	if !commit.Known || commit.Value == nil {
		return OptionalUint{Known: commit.Known}
	}
	return OptionalUint{Known: true, Value: *commit.Value}
}
