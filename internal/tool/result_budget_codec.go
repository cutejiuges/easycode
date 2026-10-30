package tool

import "easycode/internal/codec"

type resultBudgetWire struct {
	Allowance int          `json:"allowance"`
	Actual    int          `json:"actual_bytes"`
	Reason    BudgetReason `json:"reason"`
}

// MarshalJSON 序列化唯一当前预算事实，不重新计算额度。
func (v ResultBudgetMetadata) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(resultBudgetWire{v.allowance, v.actual, v.reason})
}

// DecodeResultBudgetMetadata 拒绝缺失字段和未知扩展，零值不得充当历史兼容默认。
func DecodeResultBudgetMetadata(data []byte) (ResultBudgetMetadata, error) {
	var wire resultBudgetWire
	if err := codec.DecodeObject(data, &wire, "allowance", "actual_bytes", "reason"); err != nil {
		return ResultBudgetMetadata{}, err
	}
	return NewResultBudgetMetadata(wire.Allowance, wire.Actual, wire.Reason)
}

// UnmarshalJSON 只接受当前严格格式。
func (v *ResultBudgetMetadata) UnmarshalJSON(data []byte) error {
	decoded, err := DecodeResultBudgetMetadata(data)
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}
