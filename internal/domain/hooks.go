package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	MaxHookConfigBytes    = 64 << 10
	MaxHookCommandBytes   = 8 << 10
	MaxHookInputBytes     = 512 << 10
	MaxHookOutputBytes    = 16 << 10
	MaxHookMatches        = 8
	HookShellPolicy       = "posix-sh-nonlogin"
	HookEnvironmentPolicy = "minimal-command-environment"
	HookSandboxPolicy     = "darwin-hook-workspace-readonly-offline"
)

// HookSource 是不含宿主路径的两种配置来源。
type HookSource string

const (
	HookSourceUser    HookSource = "user"
	HookSourceProject HookSource = "project"
)

func (s HookSource) Valid() bool { return s == HookSourceUser || s == HookSourceProject }

// HookCapability 只描述当前实际接入的三项独占能力。
type HookCapability string

const (
	HookPatch HookCapability = "fs.patch"
	HookWrite HookCapability = "fs.write"
	HookExec  HookCapability = "process.exec"
)

func (c HookCapability) Valid() bool { return c == HookPatch || c == HookWrite || c == HookExec }

// HookMatcher 是规范化的能力集合，不执行任意正则表达式。
type HookMatcher uint8

func ParseHookMatcher(text string) (HookMatcher, error) {
	if text == "*" {
		return 7, nil
	}
	seen := make(map[string]bool)
	var m HookMatcher
	for _, name := range strings.Split(text, "|") {
		if seen[name] {
			return 0, HookConfigInvalid
		}
		seen[name] = true
		switch name {
		case "Edit", "apply_patch":
			m |= 1
		case "Write":
			m |= 2
		case "Bash", "exec_command":
			m |= 4
		default:
			return 0, HookConfigInvalid
		}
	}
	return m, nil
}
func (m HookMatcher) Valid() bool { return m > 0 && m <= 7 }
func (m HookMatcher) Matches(c HookCapability) bool {
	switch c {
	case HookPatch:
		return m&1 != 0
	case HookWrite:
		return m&2 != 0
	case HookExec:
		return m&4 != 0
	default:
		return false
	}
}
func (m HookMatcher) String() string {
	names := make([]string, 0, 3)
	for _, c := range []HookCapability{HookPatch, HookWrite, HookExec} {
		if m.Matches(c) {
			names = append(names, string(c))
		}
	}
	return strings.Join(names, "|")
}

// HookCode 是控制流使用的封闭错误分类，不保存脚本自由文本。
type HookCode string

const (
	HookOK                 HookCode = "ok"
	HookConfigInvalid      HookCode = "hook_config_invalid"
	HookInputLimit         HookCode = "hook_input_limit"
	HookMatchLimit         HookCode = "hook_match_limit"
	HookTrustRequired      HookCode = "hook_trust_required"
	HookTrustDenied        HookCode = "hook_trust_denied"
	HookDenied             HookCode = "hook_denied"
	HookExitFailed         HookCode = "hook_exit_failed"
	HookStartFailed        HookCode = "hook_start_failed"
	HookOutputInvalid      HookCode = "hook_output_invalid"
	HookOutputLimit        HookCode = "hook_output_limit"
	HookIOFailed           HookCode = "hook_io_failed"
	HookTimedOut           HookCode = "hook_timed_out"
	HookUnsupported        HookCode = "unsupported_platform"
	HookSandboxUnavailable HookCode = "sandbox_unavailable"
	HookCancelled          HookCode = "cancelled"
	HookOutcomeUncertain   HookCode = "hook_outcome_uncertain"
	HookInterrupted        HookCode = "session_interrupted_before_hook"
)

func (c HookCode) Error() string { return string(c) }
func (c HookCode) Valid() bool {
	switch c {
	case HookOK, HookConfigInvalid, HookInputLimit, HookMatchLimit, HookTrustRequired, HookTrustDenied,
		HookDenied, HookExitFailed, HookStartFailed, HookOutputInvalid, HookOutputLimit, HookIOFailed,
		HookTimedOut, HookUnsupported, HookSandboxUnavailable, HookCancelled, HookOutcomeUncertain, HookInterrupted:
		return true
	default:
		return false
	}
}

// HookReference 是可持久化且不能用于重新执行命令的声明引用。
type HookReference struct {
	Source         HookSource `json:"source"`
	Revision       string     `json:"source_revision"`
	ID             string     `json:"id"`
	Fingerprint    string     `json:"command_fingerprint"`
	TimeoutSeconds int        `json:"timeout"`
}

func NewHookReference(source HookSource, revision, id, fingerprint string, timeout int) (HookReference, error) {
	r := HookReference{source, revision, id, fingerprint, timeout}
	return r, r.Validate()
}
func (r HookReference) Validate() error {
	if !r.Source.Valid() || !ValidHookDigest(r.Revision) || ValidateSkillName(r.ID) != nil ||
		!ValidHookDigest(r.Fingerprint) || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 30 {
		return HookConfigInvalid
	}
	return nil
}
func (r HookReference) Key() string {
	return string(r.Source) + ":" + r.Revision + ":" + r.ID + ":" + r.Fingerprint
}

// HookDeclaration 只在当前进程保存受审查的命令，不进入 Session。
type HookDeclaration struct {
	Reference HookReference `json:"reference"`
	Matcher   HookMatcher   `json:"matcher"`
	Command   string        `json:"command"`
}

func NewHookDeclaration(source HookSource, revision, id string, matcher HookMatcher, command string, timeout int) (HookDeclaration, error) {
	if !source.Valid() || !ValidHookDigest(revision) || ValidateSkillName(id) != nil || !matcher.Valid() ||
		strings.TrimSpace(command) == "" || len(command) > MaxHookCommandBytes || !utf8.ValidString(command) || strings.ContainsRune(command, 0) || timeout < 1 || timeout > 30 {
		return HookDeclaration{}, HookConfigInvalid
	}
	w := struct {
		Source                      HookSource  `json:"source"`
		ID                          string      `json:"id"`
		Matcher                     HookMatcher `json:"matcher"`
		Command                     string      `json:"command"`
		Timeout                     int         `json:"timeout"`
		Shell, Environment, Sandbox string
	}{source, id, matcher, command, timeout, HookShellPolicy, HookEnvironmentPolicy, HookSandboxPolicy}
	b, err := codec.MarshalStable(w)
	if err != nil {
		return HookDeclaration{}, HookConfigInvalid
	}
	return HookDeclaration{HookReference{source, revision, id, HookDigest(b), timeout}, matcher, command}, nil
}
func (d HookDeclaration) Validate() error {
	v, err := NewHookDeclaration(d.Reference.Source, d.Reference.Revision, d.Reference.ID, d.Matcher, d.Command, d.Reference.TimeoutSeconds)
	if err != nil || v != d {
		return HookConfigInvalid
	}
	return nil
}

// HookDigest 使用固定内容生成不可路由格式版本的摘要。
func HookDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func ValidHookDigest(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// HookPlan 冻结单个真实调用已选中的全部检查，不持有命令正文。
type HookPlan struct {
	InvocationID string          `json:"invocation_id"`
	Event        string          `json:"event"`
	Capability   HookCapability  `json:"capability"`
	Hooks        []HookReference `json:"hooks"`
	Digest       string          `json:"plan_digest"`
}

func NewHookPlan(invocation string, capability HookCapability, refs []HookReference) (HookPlan, error) {
	if _, err := parseUUIDv7(invocation); err != nil || !capability.Valid() || len(refs) == 0 || len(refs) > MaxHookMatches {
		return HookPlan{}, HookConfigInvalid
	}
	seen := make(map[string]bool)
	revisions := make(map[HookSource]string)
	projectSeen := false
	for _, r := range refs {
		key := string(r.Source) + ":" + r.ID
		if r.Validate() != nil || seen[key] || (revisions[r.Source] != "" && revisions[r.Source] != r.Revision) || projectSeen && r.Source == HookSourceUser {
			return HookPlan{}, HookConfigInvalid
		}
		seen[key] = true
		revisions[r.Source] = r.Revision
		projectSeen = projectSeen || r.Source == HookSourceProject
	}
	p := HookPlan{invocation, "PreToolUse", capability, append([]HookReference(nil), refs...), ""}
	b, err := codec.MarshalStable(p)
	if err != nil {
		return HookPlan{}, HookConfigInvalid
	}
	p.Digest = HookDigest(b)
	return p, nil
}
func (p HookPlan) Clone() HookPlan { p.Hooks = append([]HookReference(nil), p.Hooks...); return p }
func (p HookPlan) Validate() error {
	v, err := NewHookPlan(p.InvocationID, p.Capability, p.Hooks)
	if err != nil || p.Event != v.Event || p.Digest != v.Digest {
		return HookConfigInvalid
	}
	return nil
}
func DecodeHookPlan(b []byte) (HookPlan, error) {
	var p HookPlan
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &p) != nil {
		return HookPlan{}, HookConfigInvalid
	}
	return p, p.Validate()
}

// HookRunIdentity 精确关联一次检查；ordinal 来源于已冻结计划。
type HookRunIdentity struct {
	InvocationID string `json:"invocation_id"`
	PlanDigest   string `json:"plan_digest"`
	Ordinal      int    `json:"ordinal"`
	RunID        string `json:"run_id"`
}

func NewHookRunIdentity(p HookPlan, ordinal int) (HookRunIdentity, error) {
	if p.Validate() != nil || ordinal < 0 || ordinal >= len(p.Hooks) {
		return HookRunIdentity{}, HookConfigInvalid
	}
	return HookRunIdentity{p.InvocationID, p.Digest, ordinal, p.InvocationID + ":" + strconv.Itoa(ordinal)}, nil
}
func (r HookRunIdentity) Validate() error {
	_, err := parseUUIDv7(r.InvocationID)
	if err != nil || !ValidHookDigest(r.PlanDigest) || r.Ordinal < 0 || r.Ordinal >= MaxHookMatches || r.RunID != r.InvocationID+":"+strconv.Itoa(r.Ordinal) {
		return HookConfigInvalid
	}
	return nil
}

// HookOutcome 区分检查结果与目标工具执行结果。
type HookOutcome string

const (
	HookContinue  HookOutcome = "continue"
	HookBlock     HookOutcome = "denied"
	HookError     HookOutcome = "error"
	HookCancel    HookOutcome = "cancelled"
	HookUncertain HookOutcome = "outcome_uncertain"
)

func (s HookOutcome) ValidCode(c HookCode) bool {
	switch s {
	case HookContinue:
		return c == HookOK
	case HookBlock:
		return c == HookDenied || c == HookTrustDenied || c == HookTrustRequired
	case HookCancel:
		return c == HookCancelled || c == HookInterrupted
	case HookUncertain:
		return c == HookOutcomeUncertain
	case HookError:
		switch c {
		case HookConfigInvalid, HookInputLimit, HookMatchLimit, HookExitFailed, HookStartFailed, HookOutputInvalid, HookOutputLimit, HookIOFailed, HookTimedOut, HookUnsupported, HookSandboxUnavailable:
			return true
		}
	}
	return false
}

// HookRunResult 只包含允许持久化的执行统计，永不持有输出正文。
type HookRunResult struct {
	Identity         HookRunIdentity `json:"identity"`
	Status           HookOutcome     `json:"status"`
	Code             HookCode        `json:"code"`
	ExitKnown        bool            `json:"exit_known"`
	ExitCode         int             `json:"exit_code"`
	TimedOut         bool            `json:"timed_out"`
	Cancelled        bool            `json:"cancelled"`
	StdoutBytes      int64           `json:"stdout_bytes"`
	StderrBytes      int64           `json:"stderr_bytes"`
	CleanupConfirmed bool            `json:"cleanup_confirmed"`
	Origin           string          `json:"origin"`
}

func NewHookRunResult(r HookRunResult) (HookRunResult, error) { return r, r.Validate() }
func (r HookRunResult) Validate() error {
	if r.Identity.Validate() != nil || !r.Status.ValidCode(r.Code) || r.StdoutBytes < 0 || r.StderrBytes < 0 ||
		(!r.ExitKnown && r.ExitCode != 0) || r.ExitCode < -1 || r.ExitCode > 255 || (r.Origin != "execution" && r.Origin != "recovery") {
		return HookConfigInvalid
	}
	if r.Origin == "recovery" && (r.Status != HookUncertain || r.ExitKnown || r.TimedOut || r.Cancelled || r.StdoutBytes != 0 || r.StderrBytes != 0 || r.CleanupConfirmed) {
		return HookConfigInvalid
	}
	if r.Status == HookBlock && r.Code != HookDenied || r.Status == HookCancel && r.Code != HookCancelled || r.Code == HookConfigInvalid || r.Code == HookMatchLimit || r.Code == HookTrustRequired || r.Code == HookTrustDenied || r.Code == HookInterrupted {
		return HookConfigInvalid
	}
	if r.Status != HookUncertain && !r.CleanupConfirmed {
		return HookConfigInvalid
	}
	if r.Status == HookContinue && (!r.ExitKnown || r.ExitCode != 0 || r.TimedOut || r.Cancelled) {
		return HookConfigInvalid
	}
	if r.Code == HookTimedOut && !r.TimedOut || r.Code == HookCancelled && !r.Cancelled {
		return HookConfigInvalid
	}
	if r.Cancelled && r.Status != HookCancel && r.Status != HookUncertain || r.TimedOut && r.Code != HookTimedOut && r.Status != HookCancel && r.Status != HookUncertain {
		return HookConfigInvalid
	}
	switch r.Code {
	case HookDenied:
		if !r.ExitKnown || r.ExitCode != 0 && r.ExitCode != 2 {
			return HookConfigInvalid
		}
	case HookExitFailed:
		if !r.ExitKnown || r.ExitCode == 0 || r.ExitCode == 2 {
			return HookConfigInvalid
		}
	case HookOutputInvalid:
		if !r.ExitKnown || r.ExitCode != 0 {
			return HookConfigInvalid
		}
	case HookStartFailed, HookUnsupported, HookInputLimit:
		if r.ExitKnown || r.StdoutBytes != 0 || r.StderrBytes != 0 {
			return HookConfigInvalid
		}
	}
	return nil
}
func DecodeHookRunResult(b []byte) (HookRunResult, error) {
	var r HookRunResult
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &r) != nil {
		return r, HookConfigInvalid
	}
	return r, r.Validate()
}

// HookGateDecision 保存聚合结果；未执行项不冒充完成。
type HookGateDecision struct {
	InvocationID    string      `json:"invocation_id"`
	PlanDigest      string      `json:"plan_digest"`
	Status          HookOutcome `json:"status"`
	Code            HookCode    `json:"code"`
	Completed       int         `json:"completed"`
	BlockingOrdinal *int        `json:"blocking_ordinal,omitempty"`
}

func NewHookGateDecision(p HookPlan, status HookOutcome, code HookCode, completed, blocking int) (HookGateDecision, error) {
	if p.Validate() != nil || completed < 0 || completed > len(p.Hooks) || status == HookContinue && completed != len(p.Hooks) || status != HookContinue && (blocking < 0 || blocking >= len(p.Hooks) || blocking != completed && blocking+1 != completed) {
		return HookGateDecision{}, HookConfigInvalid
	}
	d := HookGateDecision{InvocationID: p.InvocationID, PlanDigest: p.Digest, Status: status, Code: code, Completed: completed}
	if status != HookContinue {
		ordinal := blocking
		d.BlockingOrdinal = &ordinal
	}
	return d, d.Validate()
}
func (d HookGateDecision) Validate() error {
	_, err := parseUUIDv7(d.InvocationID)
	if err != nil || !ValidHookDigest(d.PlanDigest) || !d.Status.ValidCode(d.Code) || d.Completed < 0 || d.Completed > MaxHookMatches {
		return HookConfigInvalid
	}
	if d.Status == HookContinue {
		if d.BlockingOrdinal != nil || d.Completed == 0 {
			return HookConfigInvalid
		}
	} else if d.BlockingOrdinal == nil || *d.BlockingOrdinal < 0 || *d.BlockingOrdinal >= MaxHookMatches {
		return HookConfigInvalid
	}
	return nil
}
func (d HookGateDecision) Clone() HookGateDecision {
	if d.BlockingOrdinal != nil {
		n := *d.BlockingOrdinal
		d.BlockingOrdinal = &n
	}
	return d
}
func DecodeHookGateDecision(b []byte) (HookGateDecision, error) {
	var d HookGateDecision
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &d) != nil {
		return d, HookConfigInvalid
	}
	return d, d.Validate()
}

// HookDiagnostic 只携带来源身份和安全分类。
type HookDiagnostic struct {
	Source HookSource
	Code   HookCode
}

// HookListEntry 是不触发 I/O 的本地检查状态投影。
type HookListEntry struct {
	Declaration HookDeclaration
	State       string
	Code        HookCode
}

// HookSnapshot 持有不可变配置；授权由 Runtime 另行持有。
type HookSnapshot struct {
	declarations []HookDeclaration
	diagnostics  []HookDiagnostic
}

func NewHookSnapshot(ds []HookDeclaration, diagnostics []HookDiagnostic) (HookSnapshot, error) {
	if len(ds) > 64 || len(diagnostics) > 2 {
		return HookSnapshot{}, HookConfigInvalid
	}
	seen := make(map[string]bool)
	counts := make(map[HookSource]int)
	revisions := make(map[HookSource]string)
	projectSeen := false
	for _, d := range ds {
		k := string(d.Reference.Source) + ":" + d.Reference.ID
		source := d.Reference.Source
		counts[source]++
		if d.Validate() != nil || seen[k] || counts[source] > 32 || source == HookSourceUser && projectSeen || revisions[source] != "" && revisions[source] != d.Reference.Revision {
			return HookSnapshot{}, HookConfigInvalid
		}
		projectSeen = projectSeen || source == HookSourceProject
		revisions[source] = d.Reference.Revision
		seen[k] = true
	}
	diagnosticSeen := make(map[HookSource]bool)
	for _, d := range diagnostics {
		if !d.Source.Valid() || d.Code != HookConfigInvalid || diagnosticSeen[d.Source] || counts[d.Source] != 0 {
			return HookSnapshot{}, HookConfigInvalid
		}
		diagnosticSeen[d.Source] = true
	}
	return HookSnapshot{append([]HookDeclaration(nil), ds...), append([]HookDiagnostic(nil), diagnostics...)}, nil
}
func (s HookSnapshot) Declarations() []HookDeclaration {
	return append([]HookDeclaration(nil), s.declarations...)
}
func (s HookSnapshot) Diagnostics() []HookDiagnostic {
	return append([]HookDiagnostic(nil), s.diagnostics...)
}
func (s HookSnapshot) Clone() HookSnapshot {
	s.declarations = s.Declarations()
	s.diagnostics = s.Diagnostics()
	return s
}
func (s HookSnapshot) Match(cap HookCapability) ([]HookDeclaration, error) {
	if !cap.Valid() {
		return nil, HookConfigInvalid
	}
	if len(s.diagnostics) > 0 {
		return nil, HookConfigInvalid
	}
	var result []HookDeclaration
	for _, d := range s.declarations {
		if d.Matcher.Matches(cap) {
			result = append(result, d)
		}
	}
	if len(result) > MaxHookMatches {
		return nil, HookMatchLimit
	}
	return result, nil
}

// HookSummary 仅由已验证标识生成不含脚本正文的模型反馈。
func HookSummary(ref HookReference, cap HookCapability, code HookCode) string {
	if ref.Validate() != nil || !cap.Valid() || !code.Valid() {
		return "Hook stopped: hook_config_invalid"
	}
	return fmt.Sprintf("Hook %s:%s stopped %s: %s. Detailed hook output was not recorded.", ref.Source, ref.ID, cap, code)
}
