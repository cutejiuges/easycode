package openai

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"reflect"
	"testing"
)

// testRestoreAfterPreview 把既有 golden 等价矩阵扩展为直接恢复与离线预览后恢复。
func testRestoreAfterPreview(t *testing.T, instance *Provider, thread domain.ThreadID, records []provider.RestoreRecord) (provider.Conversation, error) {
	t.Helper()
	direct, _, directErr := instance.RestoreConversation(context.Background(), thread, records)
	source := provider.HistorySource{ThreadID: thread, Family: domain.ProviderOpenAI, Wire: "responses", Model: instance.config.Model}
	view, previewErr := NewHistoryReader().ProjectStoredHistory(context.Background(), source, records, nil)
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
	restored, _, err := instance.RestoreConversation(context.Background(), thread, records)
	if err == nil && !reflect.DeepEqual(restored.ProjectHistory(), direct.ProjectHistory()) {
		t.Fatal("preview mutated native facts")
	}
	return restored, err
}

type recordCancelContext struct {
	context.Context
	remaining int
}

func (c *recordCancelContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func testOfflineUsageEvidence(t *testing.T, instance *Provider, prepared *provider.PreparedCompaction, checkpoint provider.CheckpointEnvelope) {
	t.Helper()
	usage, err := prepared.Usage()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := prepared.UsageProvenance()
	if err != nil {
		t.Fatal(err)
	}
	reader := NewHistoryReader()
	ctx := context.Background()
	source := provider.HistorySource{ThreadID: "01900000-0000-7000-8000-000000000001", Family: instance.Family(), Wire: instance.Family().Wire(), Model: instance.config.Model}
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
