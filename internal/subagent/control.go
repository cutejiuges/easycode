// Package subagent 定义线程树、上下文 fork 和任务控制边界。
package subagent

import (
	"context"

	"easycode/internal/domain"
)

// ForkMode 描述子代理继承父历史的方式。
type ForkMode string

const (
	ForkNone  ForkMode = "none"
	ForkFull  ForkMode = "full"
	ForkLastN ForkMode = "last_n_turns"
)

// SpawnRequest 描述创建子代理所需的稳定参数。
type SpawnRequest struct {
	ParentThreadID domain.ThreadID
	Task           string
	ForkMode       ForkMode
	LastTurns      int
}

// Control 管理同一 root session 下的子代理生命周期。
type Control interface {
	Spawn(context.Context, SpawnRequest) (domain.ThreadID, error)
	Interrupt(context.Context, domain.ThreadID) error
	Wait(context.Context, domain.ThreadID) error
}
