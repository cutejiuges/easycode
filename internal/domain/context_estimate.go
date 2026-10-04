package domain

import (
	"fmt"
	"strings"
)

// TokenEstimateState 描述上下文 token 估算是否具有可用数值。
type TokenEstimateState string

const (
	TokenEstimateEstimated TokenEstimateState = "estimated"
	TokenEstimateUnknown   TokenEstimateState = "unknown"
)

// TokenEstimate 保存不可变的本地 token 估算及其算法版本。
type TokenEstimate struct {
	method string
	state  TokenEstimateState
	tokens uint64
}

// NewEstimatedTokenEstimate 创建具有可用数值的估算。
func NewEstimatedTokenEstimate(method string, tokens uint64) (TokenEstimate, error) {
	estimate := TokenEstimate{method: strings.TrimSpace(method), state: TokenEstimateEstimated, tokens: tokens}
	if err := estimate.Validate(); err != nil {
		return TokenEstimate{}, err
	}
	return estimate, nil
}

// NewUnknownTokenEstimate 创建算法已知但数值不可判定的估算。
func NewUnknownTokenEstimate(method string) (TokenEstimate, error) {
	estimate := TokenEstimate{method: strings.TrimSpace(method), state: TokenEstimateUnknown}
	if err := estimate.Validate(); err != nil {
		return TokenEstimate{}, err
	}
	return estimate, nil
}

// Method 返回估算算法及其版本。
func (estimate TokenEstimate) Method() string { return estimate.method }

// State 返回估算状态。
func (estimate TokenEstimate) State() TokenEstimateState { return estimate.state }

// Tokens 返回 estimated 状态的数值；unknown 状态返回 false。
func (estimate TokenEstimate) Tokens() (uint64, bool) {
	return estimate.tokens, estimate.state == TokenEstimateEstimated
}

// Validate 校验估算方法、状态和值的不变量。
func (estimate TokenEstimate) Validate() error {
	if strings.TrimSpace(estimate.method) == "" {
		return fmt.Errorf("token estimate method is required")
	}
	switch estimate.state {
	case TokenEstimateEstimated:
		return nil
	case TokenEstimateUnknown:
		if estimate.tokens != 0 {
			return fmt.Errorf("unknown token estimate contains a value")
		}
		return nil
	default:
		return fmt.Errorf("token estimate state is invalid")
	}
}

// NativeHistoryFootprint 保存 Provider 已提交原生历史的只读数值摘要。
type NativeHistoryFootprint struct {
	family   ProviderFamily
	revision uint64
	estimate TokenEstimate
}

// NewNativeHistoryFootprint 创建并校验原生历史 footprint。
func NewNativeHistoryFootprint(
	family ProviderFamily,
	revision uint64,
	estimate TokenEstimate,
) (NativeHistoryFootprint, error) {
	footprint := NativeHistoryFootprint{family: family, revision: revision, estimate: estimate}
	if err := footprint.Validate(); err != nil {
		return NativeHistoryFootprint{}, err
	}
	return footprint, nil
}

// Family 返回原生历史所属 Provider 家族。
func (footprint NativeHistoryFootprint) Family() ProviderFamily { return footprint.family }

// Revision 返回已提交原生增量的单调 revision。
func (footprint NativeHistoryFootprint) Revision() uint64 { return footprint.revision }

// Estimate 返回不可变 token 估算值。
func (footprint NativeHistoryFootprint) Estimate() TokenEstimate { return footprint.estimate }

// Validate 校验 Provider 家族和 token 估算。
func (footprint NativeHistoryFootprint) Validate() error {
	if !footprint.family.Valid() {
		return fmt.Errorf("native history footprint provider family is invalid")
	}
	if err := footprint.estimate.Validate(); err != nil {
		return fmt.Errorf("native history footprint estimate is invalid: %w", err)
	}
	return nil
}
