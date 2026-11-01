package domain

import "testing"

func TestCompactionScopeValidation(t *testing.T) {
	if err := ManualCompactionScope().Validate(); err != nil {
		t.Fatal(err)
	}
	scope, err := NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []CompactionScope{{}, {Trigger: "unknown"}, {Trigger: CompactionAuto}, {Trigger: CompactionManual, TurnID: scope.TurnID}, {Trigger: CompactionManual, SampleIndex: 1}} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted %#v", invalid)
		}
	}
}
