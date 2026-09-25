// Package fault 定义稳定的英文错误码和对外错误结构。
package fault

import (
	"errors"
	"fmt"
)

// Code 是可供 CLI、JSON 客户端和未来 app-server 解析的英文错误码。
type Code string

const (
	CodeInvalidConfiguration Code = "invalid_configuration"
	CodeProviderUnavailable  Code = "provider_unavailable"
	CodeProviderRequest      Code = "provider_request_failed"
	CodeNotImplemented       Code = "not_implemented"
	CodeStreamProtocol       Code = "stream_protocol_error"
	CodeStreamIdleTimeout    Code = "stream_idle_timeout"
	CodeUserCancelled        Code = "user_cancelled"
	CodeTurnFailed           Code = "turn_failed"
)

// Error 保存稳定错误码、英文消息和可选底层原因。
type Error struct {
	Code    Code
	Message string
	Cause   error
}

// Error 返回对外英文错误文本。
func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	if err.Cause == nil {
		return fmt.Sprintf("%s: %s", err.Code, err.Message)
	}
	return fmt.Sprintf("%s: %s: %v", err.Code, err.Message, err.Cause)
}

// Unwrap 暴露底层原因供 errors.Is 和 errors.As 使用。
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Is 支持按稳定错误码或底层 cause 使用 errors.Is 判断。
func (err *Error) Is(target error) bool {
	if err == nil || target == nil {
		return false
	}
	var targetFault *Error
	if errors.As(target, &targetFault) && targetFault.Code != "" {
		return err.Code == targetFault.Code
	}
	return errors.Is(err.Cause, target)
}

// New 创建不带底层原因的对外错误。
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap 创建带底层原因的对外错误。
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}
