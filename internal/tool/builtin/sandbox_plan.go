package builtin

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/tool"
)

// SandboxPlan 是执行边界独占的不可变能力计划；绝对路径不得向上层持久化。
type SandboxPlan struct {
	root, workdir, temporary, cache string
	revision, candidateFingerprint  string
	readRoots, writeRoots           []string
}

// SandboxPlanner 只将已验证候选和描述符解析的工作区路径组合为能力计划。
type SandboxPlanner struct{}

// NewSandboxPlanner 纯内存构造固定的最小隔离策略。
func NewSandboxPlanner() *SandboxPlanner { return &SandboxPlanner{} }

// Plan 不访问文件系统；调用方必须在显式启动阶段提供实际打开描述符的路径。
func (*SandboxPlanner) Plan(candidate tool.ExecCandidate, workspaceRoot string) (SandboxPlan, error) {
	if candidate.Validate() != nil || candidate.Shell().ID() != "posix-sh-nonlogin" || candidate.Shell().Executable() != "/bin/sh" ||
		candidate.Sandbox().Revision() != "darwin-workspace-offline" || candidate.Sandbox().NetworkAllowed() || !candidate.Sandbox().WriteWorkspace() ||
		candidate.Environment().Revision() != "minimal-command-environment" ||
		!filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot || !utf8.ValidString(workspaceRoot) || strings.ContainsRune(workspaceRoot, 0) {
		return SandboxPlan{}, errSandboxUnavailable
	}
	return SandboxPlan{
		root: workspaceRoot, workdir: candidate.Invocation().Input().Workdir(), temporary: workspaceRoot, cache: filepath.Join(workspaceRoot, ".cache", "go-build"),
		revision: candidate.Sandbox().Revision(), candidateFingerprint: candidate.Fingerprint(),
		readRoots:  CommandReadRoots(workspaceRoot),
		writeRoots: []string{workspaceRoot},
	}, nil
}

// CommandReadRoots 返回当前命令策略的读取根；布局检查与实际 sandbox 共用此策略。
func CommandReadRoots(workspaceRoot string) []string {
	return []string{"/System", "/usr", "/bin", "/sbin", "/Library", "/Applications", "/opt/homebrew", "/private/etc", "/dev", workspaceRoot}
}

func (plan SandboxPlan) Workspace() string            { return plan.root }
func (plan SandboxPlan) Workdir() string              { return plan.workdir }
func (plan SandboxPlan) TemporaryDirectory() string   { return plan.temporary }
func (plan SandboxPlan) CacheDirectory() string       { return plan.cache }
func (plan SandboxPlan) Revision() string             { return plan.revision }
func (plan SandboxPlan) CandidateFingerprint() string { return plan.candidateFingerprint }
func (plan SandboxPlan) ReadRoots() []string          { return append([]string(nil), plan.readRoots...) }
func (plan SandboxPlan) WriteRoots() []string         { return append([]string(nil), plan.writeRoots...) }
func (plan SandboxPlan) NetworkAllowed() bool         { return false }
