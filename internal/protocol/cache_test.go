package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func fixtureCacheSnapshot(t *testing.T) CacheSnapshotPayload {
	t.Helper()
	data, err := os.ReadFile("testdata/cache_snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var p CacheSnapshotPayload
	if err := codec.UnmarshalRecord(data, &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCacheProtocolStrictCurrentContract(t *testing.T) {
	payload := fixtureCacheSnapshot(t)
	command, err := NewQueryCacheCommand("cache")
	if err != nil || command.Kind() != CommandQueryCache {
		t.Fatal(command, err)
	}
	result, err := NewAcceptedCommandResult(command, CommandDispositionQueried)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntryID() != "" || result.CompactionID() != "" {
		t.Fatal("query allocated an identity")
	}
	event, err := NewCacheSnapshot(testSessionID, testThreadID, payload)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewCorrelatedContextItem("cache", event)
	if err != nil {
		t.Fatal(err)
	}
	copy, _ := item.ContextEvent()
	copy.Payload[0] = 'x'
	again, _ := item.ContextEvent()
	if again.Payload[0] != '{' {
		t.Fatal("event payload aliased")
	}
	for _, replace := range []string{`"scope":null`, `"scope":"process","scope":"process"`, `"Scope":"process"`, `"scope":"process","authorization":"private"`} {
		changed := event
		changed.Payload = []byte(strings.Replace(string(event.Payload), `"scope":"process"`, replace, 1))
		if _, err := DecodeCacheSnapshot(changed); err == nil {
			t.Fatal("invalid snapshot accepted", replace)
		}
	}
	for _, count := range []int{0, 4, 6} {
		members, err := codec.ObjectMembers(event.Payload)
		if err != nil {
			t.Fatal(err)
		}
		var sources []json.RawMessage
		if err = codec.Unmarshal(members["sources"], &sources); err != nil {
			t.Fatal(err)
		}
		if count == 6 {
			sources = append(sources, sources[0])
		} else {
			sources = sources[:count]
		}
		members["sources"], err = codec.MarshalStable(sources)
		if err != nil {
			t.Fatal(err)
		}
		changed := event
		changed.Payload, err = codec.MarshalStable(members)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeCacheSnapshot(changed); err == nil {
			t.Fatal("invalid source array count accepted", count)
		}
	}
	for _, mutate := range []func(*CacheSnapshotPayload){func(p *CacheSnapshotPayload) { p.Scope = "session" }, func(p *CacheSnapshotPayload) { p.Sources[0].Kind = "future" }, func(p *CacheSnapshotPayload) { p.Sources[0].Revision = "/private/path" }, func(p *CacheSnapshotPayload) { p.Policy.Fingerprint = "bad" }, func(p *CacheSnapshotPayload) { p.LastFailure = "private provider response" }, func(p *CacheSnapshotPayload) { p.Model = "bad\x1b[2J" }, func(p *CacheSnapshotPayload) { p.Sources[3].Fingerprint = "" }} {
		p := payload
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if (CacheSnapshotPayload{}).Validate() == nil {
		t.Fatal("zero snapshot accepted")
	}
	if _, err := NewQueryCacheCommand(""); err == nil {
		t.Fatal("empty request id accepted")
	}
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(30), domain.KnownUsageMetric(70), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(5), domain.UnknownUsageMetric())
	u, _ := NewUsagePayload(usage)
	ratio := .7
	payload.LastSample = &CacheObservationPayload{Purpose: domain.RequestSample, Usage: u, ReadRatio: &ratio, ReadObserved: true, RequestFingerprint: domain.ContentFingerprint([]byte("request")), NativeFingerprint: domain.ContentFingerprint([]byte("native")), Transport: domain.TransportHTTP, Mode: domain.RequestFull, Relation: domain.NativeFirst, Reason: domain.ResetFirst, Fallback: domain.FallbackNone}
	if err := payload.Validate(); err != nil {
		t.Fatal(err)
	}
	payload.LastSample.ReadObserved = false
	if payload.Validate() == nil {
		t.Fatal("positive read observation lost")
	}
}

func TestCacheModelIdentityRedactsPathsAndBoundsLabels(t *testing.T) {
	for _, model := range []string{"/private/models/local", "C:\\Users\\private\\model", `\\private\model`, "bad\x1b[2J", strings.Repeat("a", 513), string([]byte{0xff})} {
		label := CacheModelIdentity(model)
		if label != "sha256:"+domain.ContentFingerprint([]byte(model)) {
			t.Fatal("unsafe model identity was not redacted")
		}
		payload := fixtureCacheSnapshot(t)
		payload.Model = model
		if payload.Validate() == nil {
			t.Fatal("raw unsafe model label accepted")
		}
		payload.Model = label
		if err := payload.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if CacheModelIdentity("organization/model") != "organization/model" {
		t.Fatal("ordinary model name changed")
	}
}
