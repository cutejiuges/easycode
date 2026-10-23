package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	// ExecCaptureBytesPerStream 是 stdout 或 stderr 的内存捕获上限。
	ExecCaptureBytesPerStream = 512 << 10
	// ExecModelPreviewBytes 是命令结果的模型可见字节上限。
	ExecModelPreviewBytes = 256 << 10
)

// ShellProfile 保存宿主冻结的非交互 shell 身份与执行路径。
type ShellProfile struct {
	id         string
	executable string
}

// NewShellProfile 创建不可变 shell profile。
func NewShellProfile(id, executable string) (ShellProfile, error) {
	profile := ShellProfile{id: strings.TrimSpace(id), executable: filepath.Clean(executable)}
	if err := profile.Validate(); err != nil {
		return ShellProfile{}, err
	}
	return profile, nil
}

func (profile ShellProfile) ID() string         { return profile.id }
func (profile ShellProfile) Executable() string { return profile.executable }

func (profile ShellProfile) Validate() error {
	if profile.id == "" || len(profile.id) > 128 || !utf8.ValidString(profile.id) {
		return errors.New("shell profile ID is invalid")
	}
	if profile.executable == "" || !filepath.IsAbs(profile.executable) || !utf8.ValidString(profile.executable) {
		return errors.New("shell executable must be an absolute UTF-8 path")
	}
	return nil
}

// EnvironmentPolicy 保存可继承环境名称和不会泄露实际值的稳定revision。
type EnvironmentPolicy struct {
	revision string
	allowed  []string
}

// NewEnvironmentPolicy 创建不可变环境策略。
func NewEnvironmentPolicy(revision string, allowed []string) (EnvironmentPolicy, error) {
	policy := EnvironmentPolicy{revision: strings.TrimSpace(revision), allowed: append([]string(nil), allowed...)}
	sort.Strings(policy.allowed)
	if err := policy.Validate(); err != nil {
		return EnvironmentPolicy{}, err
	}
	return policy, nil
}

func (policy EnvironmentPolicy) Revision() string { return policy.revision }
func (policy EnvironmentPolicy) AllowedNames() []string {
	return append([]string(nil), policy.allowed...)
}

func (policy EnvironmentPolicy) Validate() error {
	if policy.revision == "" || len(policy.revision) > 128 || !utf8.ValidString(policy.revision) {
		return errors.New("environment policy revision is invalid")
	}
	for index, name := range policy.allowed {
		if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "=\x00") || (index > 0 && policy.allowed[index-1] == name) {
			return errors.New("environment policy names are invalid")
		}
	}
	return nil
}

// SandboxProfile 保存稳定的命令隔离身份与能力摘要。
type SandboxProfile struct {
	revision       string
	networkAllowed bool
	writeWorkspace bool
}

// NewSandboxProfile 创建不可变sandbox profile。
func NewSandboxProfile(revision string, networkAllowed, writeWorkspace bool) (SandboxProfile, error) {
	profile := SandboxProfile{revision: strings.TrimSpace(revision), networkAllowed: networkAllowed, writeWorkspace: writeWorkspace}
	if err := profile.Validate(); err != nil {
		return SandboxProfile{}, err
	}
	return profile, nil
}

func (profile SandboxProfile) Revision() string     { return profile.revision }
func (profile SandboxProfile) NetworkAllowed() bool { return profile.networkAllowed }
func (profile SandboxProfile) WriteWorkspace() bool { return profile.writeWorkspace }
func (profile SandboxProfile) Validate() error {
	if profile.revision == "" || len(profile.revision) > 128 || !utf8.ValidString(profile.revision) {
		return errors.New("sandbox profile revision is invalid")
	}
	return nil
}

// ExecCandidate 保存approval绑定的纯内存命令候选。
type ExecCandidate struct {
	invocation  ExecInvocation
	shell       ShellProfile
	environment EnvironmentPolicy
	sandbox     SandboxProfile
	fingerprint string
}

type execCandidateCanonical struct {
	Capability          CapabilityID `json:"capability"`
	Command             string       `json:"command"`
	Workdir             string       `json:"workdir"`
	TimeoutMillis       int          `json:"timeout_ms"`
	ShellProfile        string       `json:"shell_profile"`
	EnvironmentRevision string       `json:"environment_revision"`
	SandboxRevision     string       `json:"sandbox_revision"`
	NetworkAllowed      bool         `json:"network_allowed"`
	WriteWorkspace      bool         `json:"write_workspace"`
	CaptureBytes        int          `json:"capture_bytes"`
	PreviewBytes        int          `json:"preview_bytes"`
}

// NewExecCandidate 创建并fingerprint一个不可变命令候选。
func NewExecCandidate(invocation ExecInvocation, shell ShellProfile, environment EnvironmentPolicy, sandbox SandboxProfile) (ExecCandidate, error) {
	if err := invocation.Validate(); err != nil {
		return ExecCandidate{}, err
	}
	if err := shell.Validate(); err != nil {
		return ExecCandidate{}, err
	}
	if err := environment.Validate(); err != nil {
		return ExecCandidate{}, err
	}
	if err := sandbox.Validate(); err != nil {
		return ExecCandidate{}, err
	}
	fingerprint, err := execCandidateFingerprint(invocation, shell, environment, sandbox)
	if err != nil {
		return ExecCandidate{}, err
	}
	candidate := ExecCandidate{invocation: invocation, shell: shell, environment: cloneEnvironmentPolicy(environment), sandbox: sandbox, fingerprint: fingerprint}
	if err := candidate.Validate(); err != nil {
		return ExecCandidate{}, err
	}
	return candidate, nil
}

func (candidate ExecCandidate) Invocation() ExecInvocation { return candidate.invocation }
func (candidate ExecCandidate) Shell() ShellProfile        { return candidate.shell }
func (candidate ExecCandidate) Environment() EnvironmentPolicy {
	return cloneEnvironmentPolicy(candidate.environment)
}
func (candidate ExecCandidate) Sandbox() SandboxProfile { return candidate.sandbox }
func (candidate ExecCandidate) Fingerprint() string     { return candidate.fingerprint }

func cloneEnvironmentPolicy(policy EnvironmentPolicy) EnvironmentPolicy {
	return EnvironmentPolicy{revision: policy.revision, allowed: append([]string(nil), policy.allowed...)}
}

func (candidate ExecCandidate) Validate() error {
	if err := candidate.invocation.Validate(); err != nil {
		return errors.New("Exec candidate invocation is invalid")
	}
	if candidate.shell.Validate() != nil || candidate.environment.Validate() != nil || candidate.sandbox.Validate() != nil {
		return errors.New("Exec candidate profile is invalid")
	}
	rebuilt, err := execCandidateFingerprint(candidate.invocation, candidate.shell, candidate.environment, candidate.sandbox)
	if err != nil || candidate.fingerprint == "" || rebuilt != candidate.fingerprint {
		return errors.New("Exec candidate fingerprint is invalid")
	}
	return nil
}

func execCandidateFingerprint(invocation ExecInvocation, shell ShellProfile, environment EnvironmentPolicy, sandbox SandboxProfile) (string, error) {
	input := invocation.Input()
	canonical, err := codec.MarshalCanonical(execCandidateCanonical{
		Capability: CapabilityExec, Command: input.Command(), Workdir: input.Workdir(), TimeoutMillis: input.TimeoutMillis(), ShellProfile: shell.ID(),
		EnvironmentRevision: environment.Revision(), SandboxRevision: sandbox.Revision(), NetworkAllowed: sandbox.NetworkAllowed(), WriteWorkspace: sandbox.WriteWorkspace(),
		CaptureBytes: ExecCaptureBytesPerStream, PreviewBytes: ExecModelPreviewBytes,
	}, 256<<10)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical.Bytes())
	return hex.EncodeToString(sum[:]), nil
}
