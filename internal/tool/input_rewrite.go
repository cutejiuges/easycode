package tool

import (
	"errors"
	"strings"

	"easycode/internal/domain"
)

// HookInputForInvocation 是 Runtime 与 Session 共用的单向规范参数投影。
func HookInputForInvocation(inv Invocation) (domain.HookToolInput, error) {
	if inv.Validate() != nil {
		return domain.HookToolInput{}, domain.HookConfigInvalid
	}
	switch inv.Capability() {
	case CapabilityPatch:
		p := inv.patch.Input()
		rs := p.Replacements()
		replacements := make([]domain.HookReplacement, len(rs))
		for i, r := range rs {
			replacements[i] = domain.HookReplacement{OldText: r.OldText(), NewText: r.NewText(), ReplaceAll: r.ReplaceAll(), LineBased: r.LineBased()}
		}
		return domain.NewHookPatchInput(p.FilePath(), replacements)
	case CapabilityWrite:
		return domain.NewHookWriteInput(inv.write.input.FilePath(), inv.write.input.Content())
	case CapabilityExec:
		p := inv.exec.Input()
		return domain.NewHookExecInput(p.Command(), p.Workdir(), p.TimeoutMillis())
	default:
		return domain.HookToolInput{}, domain.HookConfigInvalid
	}
}

// HookProfileForInvocation 从原调用的强类型身份导出来源约束。
func HookProfileForInvocation(inv Invocation) (domain.HookInputProfile, error) {
	if inv.Validate() != nil {
		return "", domain.HookConfigInvalid
	}
	switch inv.Capability() {
	case CapabilityPatch:
		if inv.WireKind() == WireCustomFreeform {
			return domain.HookLinePatch, nil
		}
		return domain.HookSubstringPatch, nil
	case CapabilityWrite:
		return domain.HookWriteProfile, nil
	case CapabilityExec:
		return domain.HookExecProfile, nil
	default:
		return "", domain.HookConfigInvalid
	}
}

// RewriteInvocation 保留真实调用身份，只替换同能力且来源可表达的参数。
func RewriteInvocation(original Invocation, input domain.HookToolInput) (Invocation, error) {
	profile, err := HookProfileForInvocation(original)
	if err != nil || input.ValidateReplacement(profile) != nil || string(input.Capability()) != string(original.Capability()) {
		return Invocation{}, domain.HookOutputInvalid
	}
	var call ReadyCall
	switch original.Capability() {
	case CapabilityPatch:
		file, replacements, _ := input.Patch()
		rs := make([]PatchReplacement, len(replacements))
		for i, r := range replacements {
			if r.LineBased {
				var newLines []string
				if r.NewText != "" {
					newLines = strings.Split(strings.TrimSuffix(r.NewText, "\n"), "\n")
				}
				rs[i], err = NewPatchLineReplacement(strings.Split(strings.TrimSuffix(r.OldText, "\n"), "\n"), newLines)
			} else {
				rs[i], err = NewPatchReplacement(r.OldText, r.NewText, r.ReplaceAll)
			}
			if err != nil {
				return Invocation{}, domain.HookOutputInvalid
			}
		}
		p, inputErr := NewPatchInput(file, rs)
		if inputErr != nil {
			return Invocation{}, domain.HookOutputInvalid
		}
		call, err = NewPatchReadyCall(original.ProviderCallID(), original.WireKind(), p)
	case CapabilityWrite:
		file, content, _ := input.Write()
		p, inputErr := NewWriteInput(file, content)
		if inputErr != nil {
			return Invocation{}, domain.HookOutputInvalid
		}
		call, err = NewWriteReadyCall(original.ProviderCallID(), p)
	case CapabilityExec:
		command, workdir, timeout, _ := input.Exec()
		p, inputErr := NewExecInput(command, workdir, timeout)
		if inputErr != nil {
			return Invocation{}, domain.HookOutputInvalid
		}
		call, err = NewExecReadyCall(original.ProviderCallID(), p)
	default:
		return Invocation{}, domain.HookOutputInvalid
	}
	if err != nil {
		return Invocation{}, domain.HookOutputInvalid
	}
	rewritten, err := NewInvocation(original.InvocationID(), call)
	if err != nil {
		return Invocation{}, domain.HookOutputInvalid
	}
	roundTrip, err := HookInputForInvocation(rewritten)
	if err != nil || !roundTrip.Equal(input) {
		return Invocation{}, domain.HookOutputInvalid
	}
	return rewritten, nil
}

// Equal 比较完整原调用，而非只比较 invocation ID 或能力。
func (inv Invocation) Equal(other Invocation) bool {
	if inv.Validate() != nil || other.Validate() != nil || inv.InvocationID() != other.InvocationID() || inv.ProviderCallID() != other.ProviderCallID() || inv.Capability() != other.Capability() || inv.WireKind() != other.WireKind() {
		return false
	}
	switch inv.Capability() {
	case CapabilityTaskProgress:
		return inv.task.input.Equal(other.task.input)
	case CapabilityPatch:
		return inv.patch.input.Equal(other.patch.input)
	case CapabilityWrite:
		return inv.write.input == other.write.input
	case CapabilityExec:
		return inv.exec.input == other.exec.input
	case CapabilityRead:
		return inv.read.input == other.read.input
	case CapabilityGlob:
		return inv.glob.input == other.glob.input
	case CapabilityGrep:
		return inv.grep.input == other.grep.input
	case CapabilityReadArtifact:
		return inv.artifact.input == other.artifact.input
	case CapabilitySkill:
		return inv.skill.input == other.skill.input
	case CapabilityQuestion:
		return inv.question.input.Fingerprint() == other.question.input.Fingerprint()
	default:
		return false
	}
}
func (inv Invocation) Clone() Invocation {
	inv.patch = inv.patch.clone()
	inv.task = inv.task.clone()
	return inv
}

// ReadyCall 只从同一当前调用恢复策略输入，不读取或重建 Provider history。
func (inv Invocation) ReadyCall() (ReadyCall, error) {
	if inv.Validate() != nil {
		return ReadyCall{}, errors.New("invalid invocation")
	}
	switch inv.Capability() {
	case CapabilityTaskProgress:
		return NewTaskReadyCall(inv.ProviderCallID(), inv.task.input)
	case CapabilityPatch:
		return NewPatchReadyCall(inv.ProviderCallID(), inv.WireKind(), inv.patch.input)
	case CapabilityWrite:
		return NewWriteReadyCall(inv.ProviderCallID(), inv.write.input)
	case CapabilityExec:
		return NewExecReadyCall(inv.ProviderCallID(), inv.exec.input)
	case CapabilityRead:
		return NewReadReadyCall(inv.ProviderCallID(), inv.read.input)
	case CapabilityGlob:
		return NewGlobReadyCall(inv.ProviderCallID(), inv.glob.input)
	case CapabilityGrep:
		return NewGrepReadyCall(inv.ProviderCallID(), inv.grep.input)
	case CapabilityReadArtifact:
		return NewReadArtifactReadyCall(inv.ProviderCallID(), inv.artifact.input)
	case CapabilitySkill:
		return NewSkillReadyCall(inv.ProviderCallID(), inv.skill.input)
	case CapabilityQuestion:
		return NewQuestionReadyCall(inv.ProviderCallID(), inv.question.input)
	default:
		return ReadyCall{}, errors.New("unsupported invocation")
	}
}
