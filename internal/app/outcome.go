package app

import (
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/tui"
)

// ExitClass 是应用返回给进程入口的稳定退出分类。
type ExitClass uint8

const (
	ExitSuccess ExitClass = iota
	ExitRuntimeFailure
	ExitUsageFailure
	ExitCancelled
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
	Class        ExitClass
	Report       ReportState
	Failure      fault.Summary
	BranchNotice BranchNotice
}

// BranchNotice 携带终端未确认交付的已发布或状态不确定的本地分支。
type BranchNotice struct {
	threadID    domain.ThreadID
	publication tui.BranchPublication
}

// Message 仅使用持久化分类和通过校验的 canonical ID 生成 CLI 诊断。
func (notice BranchNotice) Message() string {
	if !notice.threadID.Valid() {
		return ""
	}
	id := string(notice.threadID)
	switch notice.publication {
	case tui.BranchPublished:
		return "Session branch saved. Resume with easycode --resume " + id
	case tui.BranchPublicationUncertain:
		return "Session branch publication durability is uncertain for " + id
	default:
		return ""
	}
}

// ExitCode 返回 CLI 使用的稳定退出码。
func (outcome Outcome) ExitCode() int {
	switch outcome.Class {
	case ExitSuccess:
		return 0
	case ExitCancelled:
		return 130
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
