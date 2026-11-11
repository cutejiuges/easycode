package app

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// validateSampleUsageEvidence 在 repair 或 writer 接管前核对 Anthropic 当前计量结构。
func validateSampleUsageEvidence(plan session.ReplayPlan) error {
	if plan.SessionMetadata.Provider != domain.ProviderAnthropic {
		return nil
	}
	reader, err := storedHistoryReader(plan.SessionMetadata.Provider, plan.SessionMetadata.ProviderWire)
	if err != nil {
		return err
	}
	validator, ok := reader.(provider.SampleUsageValidator)
	if !ok {
		return fmt.Errorf("provider cannot validate sample usage")
	}
	index := 0
	for _, native := range plan.NativeCommits {
		if native.Role != domain.NativeCommitSample {
			continue
		}
		if index >= len(plan.SampleUsages) {
			return fmt.Errorf("sample usage is missing")
		}
		usage := plan.SampleUsages[index]
		if usage.Sequence != native.Sequence+1 || usage.TurnID != native.TurnID {
			return fmt.Errorf("sample usage association is invalid")
		}
		envelope, err := provider.NewNativeCommitEnvelope(native.Commit.Provider, native.Commit.Wire, native.Commit.Payload)
		if err != nil {
			return err
		}
		if err := validator.ValidateSampleUsage(envelope, usage.Usage); err != nil {
			return err
		}
		index++
	}
	if index != len(plan.SampleUsages) {
		return fmt.Errorf("sample usage has no native facts")
	}
	return nil
}
