package testsupport

import (
	"context"
	"reflect"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

// RestoreAfterPreview 把既有 golden 等价矩阵扩展为直接恢复与离线预览后恢复。
func RestoreAfterPreview(t *testing.T, instance provider.Factory, reader provider.StoredHistoryReader, source provider.HistorySource, records []provider.RestoreRecord) (provider.Conversation, error) {
	t.Helper()
	direct, _, directErr := instance.RestoreConversation(context.Background(), source.ThreadID, records)
	view, previewErr := reader.ProjectStoredHistory(context.Background(), source, records, nil)
	if (directErr == nil) != (previewErr == nil) {
		t.Fatalf("offline and direct rejection differ: %v / %v", previewErr, directErr)
	}
	if directErr != nil {
		return direct, directErr
	}
	if !reflect.DeepEqual(view, direct.ProjectHistory()) {
		t.Fatal("offline view differs from direct restore")
	}
	if len(view.Turns) > 0 && len(view.Turns[0].Messages) > 0 {
		view.Turns[0].Messages[0].Text = "changed projection"
	}
	restored, _, err := instance.RestoreConversation(context.Background(), source.ThreadID, records)
	if err == nil && !reflect.DeepEqual(restored.ProjectHistory(), direct.ProjectHistory()) {
		t.Fatal("preview mutated native facts")
	}
	return restored, err
}

type RecordCancelContext struct {
	context.Context
	Remaining int
}

func (c *RecordCancelContext) Err() error {
	c.Remaining--
	if c.Remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func OfflineUsageEvidence(t *testing.T, instance provider.Factory, reader provider.StoredHistoryReader, source provider.HistorySource, prepared *provider.PreparedCompaction, checkpoint provider.CheckpointEnvelope) {
	t.Helper()
	usage, err := prepared.Usage()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := prepared.UsageProvenance()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, online, err := instance.RestoreConversation(ctx, source.ThreadID, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkOffline := func(expected domain.SampleUsage) error {
		_, err := reader.ProjectStoredHistory(ctx, source, nil, func(checkpoints provider.CheckpointReader) error {
			return checkpoints.ValidateCheckpointUsage(checkpoint, expected)
		})
		return err
	}
	if err = checkOffline(usage); err != nil {
		t.Fatal(err)
	}
	if err = reader.ValidateCompactionUsage(provenance, usage); err != nil {
		t.Fatal(err)
	}
	wrong, err := domain.NewSampleUsage(domain.KnownUsageMetric(999999), usage.CacheRead(), usage.CacheWrite(), usage.Output(), usage.ReasoningOutput())
	if err != nil {
		t.Fatal(err)
	}
	if checkOffline(wrong) == nil || online.ValidateCheckpointUsage(checkpoint, wrong) == nil {
		t.Fatal("mismatched checkpoint usage accepted")
	}
	if reader.ValidateCompactionUsage(provenance, wrong) == nil {
		t.Fatal("mismatched failed-compaction provenance accepted")
	}
}
