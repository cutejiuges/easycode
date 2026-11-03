package context

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"math"
	"testing"
)

func diagnosticSources(t *testing.T) CacheSources {
	t.Helper()
	plan, err := NewPlanner().Plan(mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), "", DisabledBudget()))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := NewCacheSources(plan)
	if err != nil {
		t.Fatal(err)
	}
	return sources
}
func refreshDiagnosticFingerprint(t *testing.T, s *CacheSources) {
	t.Helper()
	raw, err := codec.MarshalCanonical(s.Sources, 4096)
	if err != nil {
		t.Fatal(err)
	}
	s.ContentFingerprint = domain.ContentFingerprint(raw.Bytes())
}
func TestCacheSourceChangeMatrix(t *testing.T) {
	base := diagnosticSources(t)
	for i, source := range base.Sources {
		if !source.Present {
			continue
		}
		for _, revisionOnly := range []bool{false, true} {
			next := base
			reason := "source_content_changed"
			if revisionOnly {
				next.Sources[i].Revision = "changed"
				reason = "source_revision_changed"
			} else {
				next.Sources[i].Fingerprint = domain.ContentFingerprint([]byte("changed"))
			}
			refreshDiagnosticFingerprint(t, &next)
			got := CompareCacheSources(base, next, "native", domain.RequestReceipt{NativeFingerprint: "native", Relation: domain.NativeReplaced}, false)
			if got.Reason != reason || got.Source != source.Kind {
				t.Fatal(got, reason, source.Kind)
			}
		}
	}
	added := base
	added.Sources[2] = CacheSourceStamp{Kind: SourceProjectInstructions, Present: true, Revision: "added", Fingerprint: domain.ContentFingerprint([]byte("project"))}
	refreshDiagnosticFingerprint(t, &added)
	if got := CompareCacheSources(base, added, "", domain.RequestReceipt{}, false); got.Reason != "source_added" {
		t.Fatal(got)
	}
	if got := CompareCacheSources(added, base, "", domain.RequestReceipt{}, false); got.Reason != "source_removed" {
		t.Fatal(got)
	}
	for _, test := range []struct {
		receipt  domain.RequestReceipt
		restored bool
		reason   string
	}{
		{domain.RequestReceipt{NativeFingerprint: "new", Relation: domain.NativeAppend}, false, "history_appended"},
		{domain.RequestReceipt{NativeFingerprint: "new", Relation: domain.NativeReplaced}, false, "native_context_changed"},
		{domain.RequestReceipt{Reason: domain.ResetCompacted}, false, "context_compacted"},
		{domain.RequestReceipt{}, true, "restored_baseline"},
		{domain.RequestReceipt{NativeFingerprint: "old"}, false, "unchanged"},
	} {
		if got := CompareCacheSources(base, base, "old", test.receipt, test.restored); got.Reason != test.reason {
			t.Fatal(got, test.reason)
		}
	}
	if got := CompareCacheSources(CacheSources{}, base, "", domain.RequestReceipt{}, false); got.Reason != "first_request" {
		t.Fatal(got)
	}
	copy := base
	copy.Sources[0].Revision = "/private/path"
	if copy.Validate() == nil {
		t.Fatal("path revision allowed")
	}
	if base.Validate() != nil {
		t.Fatal("copy modified source")
	}
}
func TestCacheReadRatioPreservesUncertainty(t *testing.T) {
	k, u, na := domain.KnownUsageMetric, domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric()
	for _, test := range []struct {
		name               string
		input, read, write domain.UsageMetric
		ratio              float64
		known              bool
	}{
		{"ordinary", k(30), k(70), na, .7, true}, {"write denominator", k(20), k(60), k(20), .6, true}, {"known zero", k(5), k(0), k(0), 0, true},
		{"unknown input", u, k(5), na, 0, false}, {"unknown write", k(5), k(5), u, 0, false}, {"unknown read", k(5), u, na, 0, false}, {"not applicable read", k(5), na, na, 0, false},
		{"zero denominator", k(0), k(0), na, 0, false}, {"overflow", k(math.MaxUint64), k(1), na, 0, false}, {"max", k(0), k(math.MaxUint64), na, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			usage, err := domain.NewSampleUsage(test.input, test.read, test.write, k(0), u)
			if err != nil {
				t.Fatal(err)
			}
			ratio, known := domain.CacheReadRatio(usage)
			if ratio != test.ratio || known != test.known {
				t.Fatal(ratio, known)
			}
		})
	}
}
