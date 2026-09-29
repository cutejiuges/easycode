package app

import "easycode/internal/fault"

// ExitClass 是应用返回给进程入口的稳定退出分类。
type ExitClass uint8

const (
	ExitSuccess ExitClass = iota
	ExitRuntimeFailure
	ExitUsageFailure
)

// ReportState 表示失败是否已经通过选定输出协议报告。
type ReportState uint8

const (
	ReportPending ReportState = iota
	ReportComplete
	ReportOutputUnavailable
)

// Outcome 携带退出分类和安全错误，不保留底层 cause。
type Outcome struct {
	Class   ExitClass
	Report  ReportState
	Failure fault.Summary
}

// ExitCode 返回 CLI 使用的稳定退出码。
func (outcome Outcome) ExitCode() int {
	switch outcome.Class {
	case ExitSuccess:
		return 0
	case ExitUsageFailure:
		return 2
	default:
		return 1
	}
}

func runtimeFailure(err error) Outcome {
	return Outcome{Class: ExitRuntimeFailure, Report: ReportPending, Failure: fault.Project(err)}
}

func usageFailure(message string) Outcome {
	return Outcome{
		Class: ExitUsageFailure, Report: ReportPending,
		Failure: fault.Summary{Code: fault.CodeInvalidInput, Message: message},
	}
}
