package tool

import (
	"errors"

	"easycode/internal/domain"
)

// ValidateNativeCallInput 单向核对原生调用与 O，不从规范参数反向构造 Provider 请求。
func ValidateNativeCallInput(original Invocation, family domain.ProviderFamily, name string, callID ProviderCallID, wire ProviderWireKind, arguments []byte) error {
	if original.Validate() != nil || !family.Valid() || original.ProviderCallID() != callID || original.WireKind() != wire {
		return errors.New("native call identity mismatch")
	}
	expectedName, _ := facadeIdentity(family, original.Capability())
	expectedWire := WireStructured
	if family == domain.ProviderOpenAI && original.Capability() == CapabilityPatch {
		expectedWire = WireCustomFreeform
	}
	if name != expectedName || wire != expectedWire {
		return errors.New("native call facade mismatch")
	}
	ready, err := decodeFacadeInput(family, original.Capability(), wire, callID, arguments)
	if err != nil {
		return errors.New("invalid native call input")
	}
	inv, err := NewInvocation(original.InvocationID(), ready)
	if err != nil || !inv.Equal(original) {
		return errors.New("native call input differs from original ready")
	}
	return nil
}
