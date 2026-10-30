package tool

import "testing"

func TestResultBudgetOrderedReservationAndActualCharge(t *testing.T) {
	b := NewResultBudget()
	if err := b.BeginSample(8); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		allowance, err := b.Allowance(CapabilityRead)
		if err != nil {
			t.Fatal(err)
		}
		want := min(MaxModelPreviewBytes, SamplePreviewBytes-b.SampleUsed()-(7-i)*SafetyResultBytes)
		if allowance != want {
			t.Fatalf("call %d allowance=%d want=%d", i, allowance, want)
		}
		metadata, err := NewResultBudgetMetadata(allowance, allowance, BudgetPreserved)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Commit(CapabilityRead, metadata); err != nil {
			t.Fatal(err)
		}
	}
	if b.SampleUsed() != SamplePreviewBytes {
		t.Fatal("sample budget not bounded exactly")
	}
	used := b.TurnUsed()
	if err := b.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	allowance, err := b.Allowance(CapabilityGrep)
	if err != nil || allowance != MaxSearchPreviewBytes {
		t.Fatalf("search allowance %d %v", allowance, err)
	}
	metadata, _ := NewResultBudgetMetadata(allowance, 80, BudgetPreserved)
	if err := b.Commit(CapabilityGrep, metadata); err != nil {
		t.Fatal(err)
	}
	if b.TurnUsed() != used+80 {
		t.Fatal("steering replenished turn or reservation was charged")
	}
}
func TestResultBudgetExhaustionAndSafety(t *testing.T) {
	b := NewResultBudget()
	for call := 0; call < MaxBudgetCalls; call++ {
		if err := b.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		allowance, err := b.Allowance(CapabilityExec)
		if err != nil {
			t.Fatal(err)
		}
		if b.BodyExhausted() && allowance != SafetyResultBytes {
			t.Fatal("unsafe allowance after exhaustion")
		}
		metadata, _ := NewResultBudgetMetadata(allowance, allowance, BudgetPreserved)
		if err := b.Commit(CapabilityExec, metadata); err != nil {
			t.Fatal(err)
		}
	}
	if b.TurnUsed() != TurnPreviewBytes || !b.BodyExhausted() {
		t.Fatal("turn limit not reached")
	}
	if err := b.BeginSample(1); err == nil {
		t.Fatal("accepted 65th call")
	}
}
func TestResultBudgetRejectsInvalidTransitionWithoutCharge(t *testing.T) {
	b := NewResultBudget()
	if _, err := b.Allowance(CapabilityRead); err == nil {
		t.Fatal("call without sample")
	}
	if err := b.BeginSample(-1); err == nil {
		t.Fatal("negative calls")
	}
	if err := b.BeginSample(65); err == nil {
		t.Fatal("too many calls")
	}
	if err := b.BeginSample(2); err != nil {
		t.Fatal(err)
	}
	if err := b.BeginSample(1); err == nil {
		t.Fatal("replaced unfinished sample")
	}
	if _, err := NewResultBudgetMetadata(1024, 1025, BudgetShortened); err == nil {
		t.Fatal("over budget")
	}
	if _, err := NewResultBudgetMetadata(2048, 1025, BudgetRecovery); err == nil {
		t.Fatal("oversized recovery")
	}
	if _, err := NewResultBudgetMetadata(1024, 10, "unknown"); err == nil {
		t.Fatal("unknown reason")
	}
	metadata, _ := NewResultBudgetMetadata(1024, 80, BudgetPreserved)
	if err := b.Commit(CapabilityRead, metadata); err == nil {
		t.Fatal("accepted wrong allowance")
	}
	if b.TurnUsed() != 0 || b.SampleUsed() != 0 {
		t.Fatal("failed commit charged budget")
	}
}

func TestResultBudgetExhaustionCannotBeReversedBySmallErrors(t *testing.T) {
	b := NewResultBudget()
	for !b.BodyExhausted() {
		if err := b.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		allowance, err := b.Allowance(CapabilityRead)
		if err != nil {
			t.Fatal(err)
		}
		metadata, _ := NewResultBudgetMetadata(allowance, allowance, BudgetPreserved)
		if err := b.Commit(CapabilityRead, metadata); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	allowance, err := b.Allowance(CapabilityRead)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := NewResultBudgetMetadata(allowance, 64, BudgetRejected)
	if err := b.Commit(CapabilityRead, metadata); err != nil {
		t.Fatal(err)
	}
	if !b.BodyExhausted() {
		t.Fatal("unused safety space reopened admission")
	}
}
