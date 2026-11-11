package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"fmt"
)

var _ provider.SampleUsageValidator = (*HistoryReader)(nil)

// ValidateSampleUsage 纯内存核对唯一当前原生 shape，不以任一计量覆盖另一份。
func (*HistoryReader) ValidateSampleUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	entry, err := decodeNativeCommit(envelope)
	if err != nil {
		return err
	}
	if entry.Kind != nativeHistorySample {
		return fmt.Errorf("usage requires a native sample")
	}
	actual, err := entry.Metadata.Usage.normalized()
	if err != nil || actual != expected {
		return fmt.Errorf("sample usage does not match native facts")
	}
	return nil
}
