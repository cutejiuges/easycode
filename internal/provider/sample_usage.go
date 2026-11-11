package provider

import "easycode/internal/domain"

// SampleUsageValidator 核对普通 sample 的原生计量与同批归一化事实。
type SampleUsageValidator interface {
	ValidateSampleUsage(NativeCommitEnvelope, domain.SampleUsage) error
}
