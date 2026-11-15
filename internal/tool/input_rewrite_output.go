package tool

import (
	"errors"
	"fmt"
	"strings"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const (
	HookRewriteBudgetExceeded = "hook_rewrite_budget_exceeded"
	MaxRewriteSourceBytes     = 1 << 10
	MaxRewriteShortNoteBytes  = 256
)

// InputRewriteBudgetError 是候选和副作用之前可确定拒绝的额度不足。
type InputRewriteBudgetError struct{}

func (InputRewriteBudgetError) Error() string { return HookRewriteBudgetExceeded }

// InputResultOrigin 区分本次真实收口与仅补偿既有缺口的恢复操作。
type InputResultOrigin string

const (
	InputResultExecution InputResultOrigin = "execution"
	InputResultRecovery  InputResultOrigin = "recovery"
)

// InputResultEvidence 是模型编码与恢复共用的选参、真实准入和结果来源。
type InputResultEvidence struct {
	selection InputSelection
	admission ExecutionAdmission
	origin    InputResultOrigin
}

func NewInputResultEvidence(selection InputSelection, admission ExecutionAdmission, origin InputResultOrigin) (InputResultEvidence, error) {
	v := InputResultEvidence{selection.Clone(), admission, origin}
	if err := v.Validate(); err != nil {
		return InputResultEvidence{}, err
	}
	return v, nil
}
func (v InputResultEvidence) Validate() error {
	if v.selection.Validate() != nil || v.origin != InputResultExecution && v.origin != InputResultRecovery {
		return errors.New("invalid input result evidence")
	}
	if v.admission.Sequence() == 0 {
		if v.admission.InvocationID() != "" {
			return errors.New("incomplete input result admission")
		}
	} else if v.admission.InvocationID() != v.selection.original.InvocationID() || v.selection.kind == InputGateRejected {
		return errors.New("input result admission mismatch")
	}
	return nil
}
func (v InputResultEvidence) Selection() InputSelection     { return v.selection.Clone() }
func (v InputResultEvidence) Admission() ExecutionAdmission { return v.admission }
func (v InputResultEvidence) Origin() InputResultOrigin     { return v.origin }
func (v InputResultEvidence) Clone() InputResultEvidence    { v.selection = v.selection.Clone(); return v }

type rewriteSource struct {
	Source domain.HookSource `json:"source"`
	ID     string            `json:"id"`
}
type rewriteProvenance struct {
	InputDigest string          `json:"input_digest"`
	Sources     []rewriteSource `json:"sources"`
}

func fullInputRewriteNote(selection InputSelection) (string, error) {
	if selection.Validate() != nil {
		return "", errors.New("invalid rewrite selection")
	}
	if !selection.Changed() {
		return "", nil
	}
	input, err := CheckingHookInput(selection.original, selection.plan, selection.finished)
	if err != nil {
		return "", err
	}
	digest, err := input.Digest()
	if err != nil {
		return "", err
	}
	metadata := rewriteProvenance{InputDigest: digest, Sources: make([]rewriteSource, 0, len(selection.finished))}
	for _, f := range selection.finished {
		if f.InputEffect.Kind() == domain.HookInputReplaced {
			metadata.Sources = append(metadata.Sources, rewriteSource{f.Feedback.Reference.Source, f.Feedback.Reference.ID})
		}
	}
	provenance, err := codec.MarshalStable(metadata)
	if err != nil || len(provenance) > MaxRewriteSourceBytes {
		return "", errors.New("rewrite provenance exceeds limit")
	}
	canonical, err := input.MarshalJSON()
	if err != nil || len(canonical) > domain.MaxHookRewriteBytes {
		return "", errors.New("rewrite input exceeds limit")
	}
	return "[PreToolUse input rewrite]\nAdmitted tool attempt used this effective input; admission does not confirm process startup.\n" + string(provenance) + "\n" + string(canonical) + "\n[/PreToolUse input rewrite]\n", nil
}

// InputRewriteBodyAllowance 在任何普通候选或工具准入前为完整说明预留实际 bytes。
func InputRewriteBodyAllowance(selection InputSelection, allowance int) (int, error) {
	if allowance < SafetyResultBytes || allowance > MaxModelPreviewBytes {
		return 0, errors.New("invalid rewrite allowance")
	}
	note, err := fullInputRewriteNote(selection)
	if err != nil {
		return 0, err
	}
	if len(note)+SafetyResultBytes > allowance {
		return 0, InputRewriteBudgetError{}
	}
	return allowance - len(note), nil
}

func inputRewriteNote(v InputResultEvidence, status ResultStatus) (string, error) {
	if v.Validate() != nil || !status.Valid() {
		return "", errors.New("invalid rewrite note evidence")
	}
	if !v.selection.Changed() {
		return "", nil
	}
	if v.origin == InputResultExecution && v.admission.Sequence() != 0 {
		return fullInputRewriteNote(v.selection)
	}
	input, err := CheckingHookInput(v.selection.original, v.selection.plan, v.selection.finished)
	if err != nil {
		return "", err
	}
	digest, err := input.Digest()
	if err != nil {
		return "", err
	}
	message := "PreToolUse changed input; tool was not executed."
	if v.origin == InputResultRecovery {
		message = "PreToolUse changed input; recovery did not rerun the tool; outcome is not confirmed."
	}
	note := message + " input_digest=" + digest + "; status=" + string(status) + "\n"
	if len(note) > MaxRewriteShortNoteBytes {
		return "", errors.New("rewrite safety note exceeds limit")
	}
	return note, nil
}

// ValidateResultInput 核对三种 mutation 的实际 metadata，不从模型说明反推输入。
func ValidateResultInput(inv Invocation, result InvocationResult) error {
	if inv.Validate() != nil || result.Validate() != nil || inv.InvocationID() != result.InvocationID() || inv.ProviderCallID() != result.ProviderCallID() || inv.Capability() != result.Capability() || inv.WireKind() != result.WireKind() {
		return errors.New("result input identity mismatch")
	}
	switch inv.Capability() {
	case CapabilityPatch:
		m := result.PatchMetadata()
		if m.RelativePath() != inv.patch.input.FilePath() || m.ReplacementCount() != len(inv.patch.input.replacements) {
			return errors.New("patch result input mismatch")
		}
	case CapabilityWrite:
		m := result.WriteMetadata()
		if m.RelativePath() != inv.write.input.FilePath() || result.Status() == ResultSuccess && (m.Bytes() != len(inv.write.input.Content()) || m.Lines() != WriteLineCount(inv.write.input.Content())) {
			return errors.New("write result input mismatch")
		}
	case CapabilityExec:
		if result.ExecMetadata().Workdir() != inv.exec.input.Workdir() {
			return errors.New("exec result input mismatch")
		}
	}
	return nil
}

func validateRecoveryInputResult(result InvocationResult, admission ExecutionAdmission) error {
	if result.Budget().Reason() != BudgetRecovery || result.Budget().ActualBytes() > SafetyResultBytes {
		return errors.New("invalid recovery input result budget")
	}
	allowed := false
	if admission.Sequence() == 0 && result.Status() == ResultCancelled {
		switch result.Code() {
		case "session_interrupted_before_approval", "session_interrupted_before_execution", string(domain.HookInterrupted), string(domain.HookOutcomeUncertain):
			allowed = true
		}
	}
	if admission.Sequence() != 0 && result.Status() == ResultOutcomeUncertain && result.Code() == "outcome_uncertain" {
		allowed = true
	}
	if !allowed {
		return errors.New("invalid recovery input result terminal")
	}
	if result.Capability() == CapabilityExec {
		m := result.ExecMetadata()
		_, known := m.ExitCode()
		if known || m.DurationMillis() != 0 || m.Stdout().ObservedBytes() != 0 || m.Stderr().ObservedBytes() != 0 || m.TimedOut() || len(result.ArtifactDescriptors()) != 0 {
			return errors.New("recovery input result contains execution output")
		}
	}
	return nil
}

func (v InputResultEvidence) ValidateResult(result InvocationResult) error {
	if v.Validate() != nil {
		return errors.New("invalid input result evidence")
	}
	inv, err := v.selection.ResultInvocation()
	if err != nil || ValidateResultInput(inv, result) != nil {
		return errors.New("input result metadata mismatch")
	}
	if v.selection.kind == InputGateRejected {
		decision := v.selection.decision
		status := ResultError
		switch decision.Status {
		case domain.HookBlock:
			status = ResultDenied
		case domain.HookCancel, domain.HookUncertain:
			status = ResultCancelled
		}
		if result.Status() != status || result.Code() != string(decision.Code) {
			return errors.New("input result does not match rejected gate")
		}
	}
	if v.selection.kind == InputGateSelected && v.admission.Sequence() == 0 && (result.Status() == ResultSuccess || result.Status() == ResultOutcomeUncertain) {
		return errors.New("selected input result requires execution admission")
	}
	if v.origin == InputResultRecovery {
		if err := validateRecoveryInputResult(result, v.admission); err != nil {
			return err
		}
	} else if result.Budget().Reason() == BudgetRecovery {
		return errors.New("execution result impersonates recovery")
	}
	note, err := inputRewriteNote(v, result.Status())
	if err != nil || !strings.HasPrefix(result.Preview().Text(), note) {
		return errors.New("input rewrite note mismatch")
	}
	return nil
}

// FreezeInputRewriteResult 只由提交 owner 调用一次，说明优先于普通正文占用同一 base 额度。
func FreezeInputRewriteResult(result InvocationResult, evidence InputResultEvidence, allowance int, reason BudgetReason) (InvocationResult, error) {
	if evidence.Validate() != nil || result.Validate() != nil || allowance < SafetyResultBytes || allowance > MaxModelPreviewBytes {
		return InvocationResult{}, errors.New("invalid rewrite result finalization")
	}
	inv, err := evidence.selection.ResultInvocation()
	if err != nil || ValidateResultInput(inv, result) != nil {
		return InvocationResult{}, errors.New("rewrite result input mismatch")
	}
	note, err := inputRewriteNote(evidence, result.Status())
	if err != nil {
		return InvocationResult{}, err
	}
	if note == "" {
		frozen, err := FinalizeResult(result, allowance, reason)
		if err != nil {
			return InvocationResult{}, err
		}
		return frozen, evidence.ValidateResult(frozen)
	}
	limit := allowance
	if reason == BudgetRecovery || reason == BudgetRejected {
		limit = min(limit, SafetyResultBytes)
	}
	bodyAllowance := limit - len(note)
	if bodyAllowance <= 0 {
		return InvocationResult{}, errors.New("rewrite note exceeds result allowance")
	}
	body := result
	if bodyAllowance >= SafetyResultBytes {
		body, err = FinalizeResult(body, bodyAllowance, reason)
		if err != nil {
			return InvocationResult{}, err
		}
		reason = body.Budget().Reason()
	} else if len(body.preview.text) > bodyAllowance {
		body.preview.text = fmt.Sprintf("%s: %s (%s)", body.capability, body.status, body.code)
		if len(body.preview.text) > bodyAllowance {
			return InvocationResult{}, errors.New("rewrite terminal exceeds safety allowance")
		}
		if reason != BudgetRecovery && reason != BudgetRejected {
			reason = BudgetShortened
		}
	}
	body.preview.text = note + body.preview.text
	metadata, err := NewResultBudgetMetadata(allowance, len(body.preview.text), reason)
	if err != nil {
		return InvocationResult{}, err
	}
	body, err = body.WithBudget(metadata)
	if err != nil {
		return InvocationResult{}, err
	}
	return body, evidence.ValidateResult(body)
}
