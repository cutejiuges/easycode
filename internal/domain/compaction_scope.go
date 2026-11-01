package domain

import "fmt"

// CompactionTrigger 是摘要来源的封闭分类。
type CompactionTrigger string

const (
	CompactionManual CompactionTrigger = "manual"
	CompactionAuto   CompactionTrigger = "auto"
)

// CompactionScope 绑定独立手动操作或活动回合的目标采样安全点。
type CompactionScope struct {
	Trigger     CompactionTrigger `json:"trigger"`
	TurnID      TurnID            `json:"turn_id,omitempty"`
	SampleIndex uint32            `json:"sample_index"`
}

// ManualCompactionScope 创建不关联普通回合的手动来源。
func ManualCompactionScope() CompactionScope { return CompactionScope{Trigger: CompactionManual} }

// NewAutoCompactionScope 创建活动回合的自动来源。
func NewAutoCompactionScope(turn TurnID, index uint32) (CompactionScope, error) {
	scope := CompactionScope{Trigger: CompactionAuto, TurnID: turn, SampleIndex: index}
	return scope, scope.Validate()
}

// Validate 拒绝未知来源及手动与自动身份混用。
func (scope CompactionScope) Validate() error {
	switch scope.Trigger {
	case CompactionManual:
		if scope.TurnID == "" && scope.SampleIndex == 0 {
			return nil
		}
	case CompactionAuto:
		if scope.TurnID.Valid() {
			return nil
		}
	}
	return fmt.Errorf("compaction scope is invalid")
}
