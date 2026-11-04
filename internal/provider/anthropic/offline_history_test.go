package anthropic

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
	direct, directErr := instance.RestoreConversation(thread, records)
	source := provider.HistorySource{ThreadID: thread, Family: domain.ProviderAnthropic, Wire: "messages", Model: instance.config.Model}
	view, previewErr := NewHistoryReader().ProjectStoredHistory(context.Background(), source, records)
	if (directErr == nil) != (previewErr == nil) {
		t.Fatalf("offline and direct rejection differ: %v / %v", previewErr, directErr)
	}
	if directErr != nil {
		return direct, directErr
	}
	if !reflect.DeepEqual(view, direct.ProjectHistory()) {
		t.Fatal("offline view differs from direct restore")
	}
	for boundary := 1; boundary <= len(records)+2; boundary++ {
		ctx := &recordCancelContext{Context: context.Background(), remaining: boundary}
		got, err := NewHistoryReader().ProjectStoredHistory(ctx, source, records)
		if err == nil || len(got.Turns) != 0 {
			t.Fatalf("cancellation boundary %d leaked view", boundary)
		}
	}
	if len(view.Turns) > 0 && len(view.Turns[0].Messages) > 0 {
		view.Turns[0].Messages[0].Text = "changed projection"
	}
	restored, err := instance.RestoreConversation(thread, records)
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
func TestOfflineHistorySourceValidation(t *testing.T) {
	reader := NewHistoryReader()
	valid := provider.HistorySource{ThreadID: "01900000-0000-7000-8000-000000000001", Family: domain.ProviderAnthropic, Wire: "messages", Model: "model"}
	empty, err := reader.ProjectStoredHistory(context.Background(), valid, nil)
	if err != nil || len(empty.Turns) != 0 {
		t.Fatal("empty history rejected", err)
	}
	for _, field := range []string{"thread", "family", "wire", "model"} {
		source := valid
		switch field {
		case "thread":
			source.ThreadID = "bad"
		case "family":
			source.Family = "unknown"
		case "wire":
			source.Wire = "other"
		case "model":
			source.Model = ""
		}
		if _, err := reader.ProjectStoredHistory(context.Background(), source, nil); err == nil {
			t.Fatalf("accepted invalid %s", field)
		}
	}
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
	if err = reader.ValidateCheckpointUsage(checkpoint, usage); err != nil {
		t.Fatal(err)
	}
	if err = reader.ValidateCompactionUsage(provenance, usage); err != nil {
		t.Fatal(err)
	}
	wrong, err := domain.NewSampleUsage(domain.KnownUsageMetric(999999), usage.CacheRead(), usage.CacheWrite(), usage.Output(), usage.ReasoningOutput())
	if err != nil {
		t.Fatal(err)
	}
	if reader.ValidateCheckpointUsage(checkpoint, wrong) == nil || instance.ValidateCheckpointUsage(checkpoint, wrong) == nil {
		t.Fatal("mismatched checkpoint usage accepted")
	}
	if reader.ValidateCompactionUsage(provenance, wrong) == nil || instance.ValidateCompactionUsage(provenance, wrong) == nil {
		t.Fatal("mismatched failed-compaction provenance accepted")
	}
}
