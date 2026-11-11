package domain

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
)

// DefaultAnthropicOutputTokens 是未显式配置的普通 Messages 输出上限。
const DefaultAnthropicOutputTokens = 4096

// MaxInferenceOutputTokens 是本切片的本地输出配置边界。
const MaxInferenceOutputTokens = 65536

// ThinkingMode 区分未发送与三种原生 thinking 选择。
type ThinkingMode string

const (
	ThinkingOmit     ThinkingMode = ""
	ThinkingDisabled ThinkingMode = "disabled"
	ThinkingAdaptive ThinkingMode = "adaptive"
	ThinkingEnabled  ThinkingMode = "enabled"
)

// ThinkingDisplay 是明确请求的正文显示策略；空值保持服务端默认。
type ThinkingDisplay string

const (
	DisplaySummarized ThinkingDisplay = "summarized"
	DisplayOmitted    ThinkingDisplay = "omitted"
)

// AnthropicEffort、OpenAIEffort 和 ReasoningSummary 保留各自原生枚举边界。
type AnthropicEffort string
type OpenAIEffort string
type ReasoningSummary string

// ThinkingSettings 是纯值且不可变的原生判别联合。
type ThinkingSettings struct {
	mode    ThinkingMode
	budget  uint64
	display ThinkingDisplay
}

// NewThinkingSettings 校验分支，不推断模型能力或修改额度。
func NewThinkingSettings(mode ThinkingMode, budget uint64, display ThinkingDisplay) (ThinkingSettings, error) {
	t := ThinkingSettings{mode, budget, display}
	if err := t.Validate(); err != nil {
		return ThinkingSettings{}, err
	}
	return t, nil
}

// Validate 拒绝未实现分支和跨分支成员。
func (t ThinkingSettings) Validate() error {
	if t.display != "" && t.display != DisplaySummarized && t.display != DisplayOmitted {
		return fmt.Errorf("thinking display is invalid")
	}
	switch t.mode {
	case ThinkingOmit, ThinkingDisabled:
		if t.budget != 0 || t.display != "" {
			return fmt.Errorf("thinking branch contains incompatible fields")
		}
	case ThinkingAdaptive:
		if t.budget != 0 {
			return fmt.Errorf("adaptive thinking cannot contain a budget")
		}
	case ThinkingEnabled:
		if t.budget < 1024 {
			return fmt.Errorf("thinking budget must be at least 1024")
		}
	default:
		return fmt.Errorf("thinking mode is invalid")
	}
	return nil
}

// Mode、BudgetTokens 和 Display 只读取冻结值。
func (t ThinkingSettings) Mode() ThinkingMode       { return t.mode }
func (t ThinkingSettings) BudgetTokens() uint64     { return t.budget }
func (t ThinkingSettings) Display() ThinkingDisplay { return t.display }

// InferenceSettings 是启动时冻结的唯一普通请求策略；显式来源不进入规范身份。
type InferenceSettings struct {
	family          ProviderFamily
	thinking        ThinkingSettings
	anthropicEffort AnthropicEffort
	openaiEffort    OpenAIEffort
	summary         ReasoningSummary
	output          uint64
}

// NewAnthropicInference 创建 Anthropic 策略；output 为零表示未显式配置。
func NewAnthropicInference(thinking ThinkingSettings, effort AnthropicEffort, output uint64) (InferenceSettings, error) {
	s := InferenceSettings{family: ProviderAnthropic, thinking: thinking, anthropicEffort: effort, output: output}
	if err := s.Validate(); err != nil {
		return InferenceSettings{}, err
	}
	return s, nil
}

// NewOpenAIInference 创建 OpenAI 策略；空成员保持不发送。
func NewOpenAIInference(effort OpenAIEffort, summary ReasoningSummary, output uint64) (InferenceSettings, error) {
	s := InferenceSettings{family: ProviderOpenAI, openaiEffort: effort, summary: summary, output: output}
	if err := s.Validate(); err != nil {
		return InferenceSettings{}, err
	}
	return s, nil
}

// DefaultInferenceSettings 创建指定 family 的缺省策略。
func DefaultInferenceSettings(family ProviderFamily) (InferenceSettings, error) {
	s := InferenceSettings{family: family}
	if err := s.Validate(); err != nil {
		return InferenceSettings{}, err
	}
	return s, nil
}

// ResolveInferenceSettings 仅将未初始化的配置值解析为指定 family 的默认值。
func ResolveInferenceSettings(family ProviderFamily, s InferenceSettings) (InferenceSettings, error) {
	if s == (InferenceSettings{}) {
		return DefaultInferenceSettings(family)
	}
	if s.family != family {
		return InferenceSettings{}, fmt.Errorf("inference settings do not match provider")
	}
	if err := s.Validate(); err != nil {
		return InferenceSettings{}, err
	}
	return s, nil
}

// Validate 校验普通额度及封闭枚举，不执行任何外部操作。
func (s InferenceSettings) Validate() error {
	if !s.family.Valid() || s.output > MaxInferenceOutputTokens {
		return fmt.Errorf("inference settings are invalid")
	}
	if err := s.thinking.Validate(); err != nil {
		return err
	}
	switch s.family {
	case ProviderAnthropic:
		if s.openaiEffort != "" || s.summary != "" {
			return fmt.Errorf("inference settings do not match provider")
		}
		switch s.anthropicEffort {
		case "", "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("Anthropic effort is invalid")
		}
		if s.thinking.mode == ThinkingDisabled && (s.anthropicEffort == "xhigh" || s.anthropicEffort == "max") {
			return fmt.Errorf("disabled thinking is incompatible with effort")
		}
		if s.thinking.mode == ThinkingEnabled && s.thinking.budget >= s.MaxOutputTokens() {
			return fmt.Errorf("thinking budget must be less than output tokens")
		}
	case ProviderOpenAI:
		if s.thinking != (ThinkingSettings{}) || s.anthropicEffort != "" {
			return fmt.Errorf("inference settings do not match provider")
		}
		switch s.openaiEffort {
		case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("OpenAI effort is invalid")
		}
		switch s.summary {
		case "", "auto", "concise", "detailed":
		default:
			return fmt.Errorf("reasoning summary is invalid")
		}
	}
	return nil
}

// Family、Thinking、AnthropicEffort、OpenAIEffort 和 Summary 返回值拷贝。
func (s InferenceSettings) Family() ProviderFamily           { return s.family }
func (s InferenceSettings) Thinking() ThinkingSettings       { return s.thinking }
func (s InferenceSettings) AnthropicEffort() AnthropicEffort { return s.anthropicEffort }
func (s InferenceSettings) OpenAIEffort() OpenAIEffort       { return s.openaiEffort }
func (s InferenceSettings) Summary() ReasoningSummary        { return s.summary }

// ExplicitOutput 区分用户选择和默认 wire 上限。
func (s InferenceSettings) ExplicitOutput() (uint64, bool) { return s.output, s.output != 0 }

// MaxOutputTokens 返回普通 wire 上限；OpenAI 零值表示不发送。
func (s InferenceSettings) MaxOutputTokens() uint64 {
	if s.family == ProviderAnthropic && s.output == 0 {
		return DefaultAnthropicOutputTokens
	}
	return s.output
}

// HasReasoning 判断是否存在显式推理选择，不代表远端已接受。
func (s InferenceSettings) HasReasoning() bool {
	return s.thinking.mode != ThinkingOmit || s.anthropicEffort != "" || s.openaiEffort != "" || s.summary != ""
}

// RequestPolicyIdentity 返回非默认 wire 属性的规范身份；默认保持完整省略。
func (s InferenceSettings) RequestPolicyIdentity() (json.RawMessage, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	output := s.output
	if s.family == ProviderAnthropic && output == DefaultAnthropicOutputTokens {
		output = 0
	}
	if !s.HasReasoning() && output == 0 {
		return nil, nil
	}
	type thinkingIdentity struct {
		Type    ThinkingMode    `json:"type"`
		Budget  uint64          `json:"budget_tokens,omitempty"`
		Display ThinkingDisplay `json:"display,omitempty"`
	}
	var thinking *thinkingIdentity
	if s.thinking.mode != ThinkingOmit {
		thinking = &thinkingIdentity{s.thinking.mode, s.thinking.budget, s.thinking.display}
	}
	identity, err := codec.MarshalCanonical(struct {
		Thinking        *thinkingIdentity `json:"thinking,omitempty"`
		AnthropicEffort AnthropicEffort   `json:"anthropic_effort,omitempty"`
		OpenAIEffort    OpenAIEffort      `json:"openai_effort,omitempty"`
		Summary         ReasoningSummary  `json:"summary,omitempty"`
		Output          uint64            `json:"max_output_tokens,omitempty"`
	}{thinking, s.anthropicEffort, s.openaiEffort, s.summary, output}, 4096)
	return identity.Bytes(), err
}
