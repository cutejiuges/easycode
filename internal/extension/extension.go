// Package extension 定义本轮真实消费的插件技能来源边界。
package extension

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension/localfile"
)

// SourceError 是来源边界只含稳定英文码的失败。
type SourceError string

func (e SourceError) Error() string { return string(e) }
func (e SourceError) Code() string  { return string(e) }

// SourceHandle 拥有一次来源借用及该次所有文件，Close 完成后才归还借用。
type SourceHandle interface {
	Read(context.Context, string, int) (localfile.Content, error)
	Entries(context.Context, int) ([]localfile.Entry, error)
	Identity() localfile.Identity
	Close() error
}

// SourceOpener 显式验证冻结来源并打开一个受限相对目录。
type SourceOpener interface {
	Open(context.Context, string) (SourceHandle, error)
}

// Contribution 是经过 manifest 校验的单插件技能根快照。
type Contribution struct {
	Source   domain.SkillSource
	Revision string
	Roots    []string
	Default  bool
	Opener   SourceOpener
}

func (c Contribution) Clone() Contribution {
	c.Roots = append([]string(nil), c.Roots...)
	return c
}

// PluginSource 保留成功和失败 manifest 的安全身份，不包含宿主路径。
type PluginSource struct {
	ID, Kind, Version, Description, Code string
	Argument                             int
	Conflict                             bool
}

// PluginSummary 是 Skill 发现、冲突及目录纳入之后的最终汇总。
type PluginSummary struct {
	ID          string
	Status      domain.PluginStatus
	Available   int
	Diagnostics []domain.PluginDiagnostic
	Omitted     int
}
