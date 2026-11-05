package builtin

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// launchPolicy 将安全目录句柄取得后的平台计划与启动骨架分离。
type launchPolicy interface {
	Plan(string) (SandboxPlan, error)
}
type execLaunchPolicy struct{ candidate tool.ExecCandidate }

func (p execLaunchPolicy) Plan(root string) (SandboxPlan, error) {
	return NewSandboxPlanner().Plan(p.candidate, root)
}

type hookLaunchPolicy struct{ fingerprint string }

func (p hookLaunchPolicy) Plan(root string) (SandboxPlan, error) {
	if !domain.ValidHookDigest(p.fingerprint) || !filepath.IsAbs(root) || filepath.Clean(root) != root || !utf8.ValidString(root) || strings.ContainsRune(root, 0) {
		return SandboxPlan{}, errSandboxUnavailable
	}
	return SandboxPlan{root: root, workdir: ".", temporary: root, cache: filepath.Join(root, ".cache", "go-build"), revision: domain.HookSandboxPolicy, candidateFingerprint: p.fingerprint, readRoots: CommandReadRoots(root)}, nil
}

type commandLaunchSpec struct {
	command, workdir string
	pipeInput        bool
	policy           launchPolicy
}
