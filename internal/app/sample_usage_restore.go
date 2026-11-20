package app

import (
	"context"
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// validateSampleUsageEvidence 在 repair 或 writer 接管前按绑定的策略核对当前计量结构。
func validateSampleUsageEvidence(ctx context.Context, validator provider.SampleUsageValidator, plan session.ReplayPlan) error {
	index := 0
	for _, native := range plan.NativeCommits {
		if err := ctx.Err(); err != nil {
			return err
		}
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
	return ctx.Err()
}
