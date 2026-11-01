package config

import (
	"bytes"
	"encoding/json"

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

type optionalBool struct {
	value   bool
	present bool
}

func decodeAutoCompact(raw json.RawMessage) (optionalBool, error) {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return optionalBool{true, true}, nil
	case "false":
		return optionalBool{false, true}, nil
	default:
		return optionalBool{}, fault.New(fault.CodeInvalidConfiguration, "auto compact must be a boolean")
	}
}

// AutoCompactionPolicy 返回仅由显式配置派生的自动调度策略。
func (config Config) AutoCompactionPolicy() (contextplan.AutoCompactionPolicy, error) {
	enabled := true
	if config.autoCompact.present {
		enabled = config.autoCompact.value
	}
	if config.autoCompact.present && enabled && !config.ContextBudget.Enabled() {
		return contextplan.AutoCompactionPolicy{}, fault.New(fault.CodeInvalidConfiguration, "auto compact requires a context window")
	}
	return contextplan.NewAutoCompactionPolicy(enabled, config.ContextBudget, config.CompactionOutputTokens())
}
