package domain

import "fmt"

// EffortLevel 是两家已实现原生档位的有限并集，不表示远端能力。
type EffortLevel string

// EffortMode 区分恢复启动值、明确省略和显式档位。
type EffortMode string

const (
	EffortReset EffortMode = "reset"
	EffortAuto  EffortMode = "auto"
	EffortValue EffortMode = "value"
)

// EffortErrorKind 区分档位非法与冻结设置冲突。
type EffortErrorKind string

const (
	EffortInvalid  EffortErrorKind = "invalid_effort"
	EffortConflict EffortErrorKind = "effort_conflict"
)

// EffortError 只保留安全类别，不携带输入正文。
type EffortError struct{ kind EffortErrorKind }

func (e *EffortError) Error() string {
	if e.kind == EffortConflict {
		return "effort conflicts with inference settings"
	}
	return "effort is invalid"
}

// Kind 返回可供控制边界映射的类别。
func (e *EffortError) Kind() EffortErrorKind { return e.kind }

// EffortChoice 是不可变、封闭的进程内选择。
type EffortChoice struct {
	mode  EffortMode
	level EffortLevel
}

// NewResetEffortChoice 明确恢复启动选择。
func NewResetEffortChoice() EffortChoice { return EffortChoice{mode: EffortReset} }

// NewAutomaticEffortChoice 明确省略原生 effort 字段。
func NewAutomaticEffortChoice() EffortChoice { return EffortChoice{mode: EffortAuto} }

// NewExplicitEffortChoice 创建显式档位，family 约束由 Apply 校验。
func NewExplicitEffortChoice(level EffortLevel) (EffortChoice, error) {
	c := EffortChoice{mode: EffortValue, level: level}
	return c, c.Validate()
}

// Mode 和 Level 返回选择的纯值。
func (c EffortChoice) Mode() EffortMode   { return c.mode }
func (c EffortChoice) Level() EffortLevel { return c.level }

// Validate 拒绝未知分支和跨分支字段。
func (c EffortChoice) Validate() error {
	switch c.mode {
	case EffortReset, EffortAuto:
		if c.level == "" {
			return nil
		}
	case EffortValue:
		switch c.level {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max":
			return nil
		}
	}
	return &EffortError{kind: EffortInvalid}
}

// Apply 只替换 effort，保留原始输出额度及其显式来源。
func (c EffortChoice) Apply(startup InferenceSettings) (InferenceSettings, error) {
	if c.Validate() != nil || startup.Validate() != nil {
		return InferenceSettings{}, &EffortError{kind: EffortInvalid}
	}
	if c.mode == EffortReset {
		return startup, nil
	}
	level := c.level
	if c.mode == EffortAuto {
		level = ""
	}
	next := startup
	switch startup.family {
	case ProviderAnthropic:
		if level == "none" || level == "minimal" {
			return InferenceSettings{}, &EffortError{kind: EffortInvalid}
		}
		next.anthropicEffort = AnthropicEffort(level)
	case ProviderOpenAI:
		next.openaiEffort = OpenAIEffort(level)
	}
	if next.Validate() != nil {
		return InferenceSettings{}, &EffortError{kind: EffortConflict}
	}
	return next, nil
}

// Effort 返回实际编译的原生档位；空值表示字段省略。
func (s InferenceSettings) Effort() EffortLevel {
	if s.family == ProviderAnthropic {
		return EffortLevel(s.anthropicEffort)
	}
	return EffortLevel(s.openaiEffort)
}

// EffortLevels 返回当前原生顺序的独立切片，不读取模型目录。
func EffortLevels(family ProviderFamily) ([]EffortLevel, error) {
	switch family {
	case ProviderAnthropic:
		return []EffortLevel{"low", "medium", "high", "xhigh", "max"}, nil
	case ProviderOpenAI:
		return []EffortLevel{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, nil
	default:
		return nil, fmt.Errorf("effort provider is invalid")
	}
}
