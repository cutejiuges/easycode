package openai

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

var _ provider.SampleUsageValidator = (*HistoryReader)(nil)

// ValidateSampleUsage 将归一化计量与已存储的普通原生样本对账。
func (*HistoryReader) ValidateSampleUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	if envelope.Family() != domain.ProviderOpenAI || envelope.Wire() != responsesWire {
		return fmt.Errorf("OpenAI native commit boundary is incompatible")
	}
	// 这里只读取计量证据，原生条目内容和时序仍由回放按原顺序校验。
	fields, err := codec.ObjectMembers(envelope.Payload())
	if err != nil {
		return err
	}
	var kind nativeHistoryEntryKind
	if err := codec.UnmarshalRecord(fields["kind"], &kind); err != nil {
		return err
	}
	if kind != nativeHistorySample {
		return fmt.Errorf("usage requires a native sample")
	}
	var stored rawUsageCommit
	if err := codec.UnmarshalRecord(fields["usage"], &stored); err != nil {
		return err
	}
	usage, err := decodeRawUsage(&stored)
	if err != nil {
		return err
	}
	actual, err := usage.normalized()
	if err != nil || actual != expected {
		return fmt.Errorf("sample usage does not match native facts")
	}
	return nil
}
