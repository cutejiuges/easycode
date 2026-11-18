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
