package config

import (
	contextplan "easycode/internal/context"
	"easycode/internal/fault"
)

// CompactionOutputTokens 返回摘要实际输出上限，省略配置时采用独立默认值。
func (config Config) CompactionOutputTokens() uint64 {
	if config.compactionOutput.present {
		return config.compactionOutput.value
	}
	return 4096
}

func validateCompactionOutput(output optionalUint64, budget contextplan.Budget) error {
	if !output.present {
		return nil
	}
	if output.value == 0 || output.value > 65536 {
		return fault.New(fault.CodeInvalidConfiguration, "compaction output tokens must be between 1 and 65536")
	}
	if budget.Enabled() && (budget.SafetyMargin() >= budget.Window() || output.value >= budget.Window()-budget.SafetyMargin()) {
		return fault.New(fault.CodeInvalidConfiguration, "compaction output reserve must be less than the context window")
	}
	return nil
}
