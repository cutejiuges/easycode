package fault

import (
	"context"
	"errors"
)

// Summary 是允许输出到 CLI 或机器协议的安全错误摘要。
type Summary struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	Cancelled bool   `json:"cancelled"`
}

// Project 将内部错误投影为不包含底层 cause 的稳定摘要。
func Project(err error) Summary {
	if errors.Is(err, context.Canceled) {
		return Summary{Code: CodeUserCancelled, Message: "turn was cancelled", Cancelled: true}
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Code != "" && typed.Message != "" {
		return Summary{
			Code: typed.Code, Message: typed.Message,
			Cancelled: typed.Code == CodeUserCancelled,
		}
	}
	return Summary{Code: CodeTurnFailed, Message: "turn failed"}
}
