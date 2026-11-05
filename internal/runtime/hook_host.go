package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

func (s *ChatSession) HookEntries() []domain.HookListEntry { return s.runtime.HookEntries() }
func (s *ChatSession) ResolveHookTrust(ctx context.Context, turn domain.TurnID, request protocol.HookTrustRequestedPayload, decision protocol.HookTrustDecision) error {
	response, err := protocol.NewHookTrustResponse(s.runtime.sessionID, s.runtime.threadID, turn, request, decision)
	if err != nil {
		return err
	}
	command, err := protocol.NewResolveHookTrustCommand(s.requestID("host-hook-trust"), response)
	if err != nil {
		return err
	}
	result, err := s.loop.Submit(ctx, command)
	if err != nil {
		return err
	}
	if result.Status() != protocol.CommandStatusAccepted {
		return fault.New(fault.CodeInvalidInput, "hook trust response rejected")
	}
	return nil
}
func (s *ChatSession) ResetHookTrust(ctx context.Context, ref domain.HookReference) error {
	target, err := protocol.NewHookTrustTarget(s.runtime.sessionID, s.runtime.threadID, ref)
	if err != nil {
		return err
	}
	command, err := protocol.NewResetHookTrustCommand(s.requestID("host-hook-reset"), target)
	if err != nil {
		return err
	}
	result, err := s.loop.Submit(ctx, command)
	if err != nil {
		return err
	}
	if result.Status() != protocol.CommandStatusAccepted {
		return fault.New(fault.CodeInvalidInput, "hook trust reset rejected")
	}
	return nil
}
