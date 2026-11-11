package runtime

import (
	"context"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

type budgetRejectedCompaction struct {
	*compactionConversation
	preflights int
}

func (c *budgetRejectedCompaction) PreflightCompaction(provider.CompactionInput) (provider.CompactionPreview, error) {
	c.preflights++
	return provider.CompactionPreview{}, provider.NewCompactionError(provider.CompactionBudgetExceeded)
}

func TestReasoningCompactionBudgetMapsAndSuppressesBeforeAdmission(t *testing.T) {
	c := &budgetRejectedCompaction{compactionConversation: newCompactionConversation()}
	j := &fakeJournal{}
	r := newTestRuntime(t, c, j)
	r.budget, _ = contextplan.NewBudget(60000, 0, 0)
	r.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, r.budget, 4096)
	if _, code := r.preflightCompaction(""); code != protocol.ControlErrorCompactionBudgetExceeded {
		t.Fatal("wrong rejection", code)
	}
	c.preflights = 0
	for i := 0; i < 2; i++ {
		if err := r.prepareAutomaticSample(context.Background(), runtimeTurnID, 1, nil, func(protocol.Event) { t.Fatal("preflight emitted durable facts") }); err != nil {
			t.Fatal("budget became protocol error", err)
		}
	}
	if c.preflights != 1 || c.generated.Load() != 0 || r.autoCompactionFailures != 1 || len(j.batches) != 0 {
		t.Fatal("preflight admitted generation or failed to suppress", c.preflights, r.autoCompactionFailures)
	}
	view, _ := r.conversation.ProjectActiveContext()
	footprint, _ := r.conversation.HistoryFootprint()
	if _, _, err := r.planHistory(view, footprint); err != nil {
		t.Fatal("legal old window blocked", err)
	}
}
