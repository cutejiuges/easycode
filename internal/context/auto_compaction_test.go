package context

import (
	"math"
	"testing"

	"easycode/internal/domain"
)

func TestAutoCompactionThreshold(t *testing.T) {
	for _, test := range []struct {
		name                                   string
		window, reserve, safety, summary, want uint64
		enabled                                bool
	}{
		{"summary reserve", 10000, 1000, 1000, 2000, 5600, true},
		{"ordinary reserve", 10000, 2000, 1000, 1000, 5600, true},
		{"small window", 2048, 0, 0, 4096, 0, false},
		{"fraction", 101, 0, 0, 1, 80, true},
		{"maximum", math.MaxUint64, 0, 0, 0, 14757395258967641292, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget, err := NewBudget(test.window, test.reserve, test.safety)
			if err != nil {
				t.Fatal(err)
			}
			policy, err := NewAutoCompactionPolicy(true, budget, test.summary)
			if err != nil {
				t.Fatal(err)
			}
			threshold, enabled := policy.Threshold()
			if threshold != test.want || enabled != test.enabled {
				t.Fatalf("threshold=%d enabled=%v", threshold, enabled)
			}
			if !enabled {
				return
			}
			for _, delta := range []int{-1, 0, 1} {
				tokens := threshold
				if delta < 0 {
					tokens--
				} else if delta > 0 {
					tokens++
				}
				estimate, _ := domain.NewEstimatedTokenEstimate("test", tokens)
				got, err := policy.Decide(estimate, false)
				if err != nil {
					t.Fatal(err)
				}
				want := AutoCompactionEligible
				if delta < 0 {
					want = AutoCompactionBelowThreshold
				}
				if got != want {
					t.Fatalf("delta=%d got=%s", delta, got)
				}
			}
		})
	}
}

func TestAutoCompactionUncertaintyAndSuppression(t *testing.T) {
	budget, _ := NewBudget(10000, 0, 0)
	policy, _ := NewAutoCompactionPolicy(true, budget, 4096)
	unknown, _ := domain.NewUnknownTokenEstimate("test")
	if got, _ := policy.Decide(unknown, false); got != AutoCompactionUnknown {
		t.Fatal(got)
	}
	if got, _ := policy.Decide(unknown, true); got != AutoCompactionSuppressed {
		t.Fatal(got)
	}
	for _, budget := range []Budget{budget, DisabledBudget()} {
		disabled, _ := NewAutoCompactionPolicy(false, budget, 4096)
		if got, _ := disabled.Decide(unknown, false); got != AutoCompactionDisabled {
			t.Fatal(got)
		}
	}
	if _, err := policy.Decide(domain.TokenEstimate{}, false); err == nil {
		t.Fatal("invalid estimate accepted")
	}
	if _, err := NewAutoCompactionPolicy(true, Budget{enabled: true}, 4096); err == nil {
		t.Fatal("invalid budget accepted")
	}
}
