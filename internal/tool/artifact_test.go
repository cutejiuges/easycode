package tool

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func testArtifactDescriptor(t *testing.T, complete bool) ArtifactDescriptor {
	t.Helper()
	d, err := NewArtifactDescriptor("0123456789abcdef0123456789abcdef", domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"), InvocationID("018f1d8a-7b5c-7def-8123-456789abcdef"), ArtifactStdout, 3, 3, strings.Repeat("a", 64), complete)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReadArtifactStrictInput(t *testing.T) {
	const valid = `{"artifact_id":"0123456789abcdef0123456789abcdef"}`
	v, err := DecodeReadArtifactInput([]byte(valid))
	if err != nil || v.Offset() != 0 || v.Limit() != 65536 {
		t.Fatalf("defaults: %v %v", v, err)
	}
	for _, suffix := range []string{`,"offset":null}`, `,"limit":null}`, `,"offset":-1}`, `,"offset":1.5}`, `,"offset":18446744073709551616}`, `,"limit":0}`, `,"limit":65537}`, `,"offset":0,"offset":1}`, `,"path":"secret"}`} {
		if _, err := DecodeReadArtifactInput([]byte(strings.TrimSuffix(valid, "}") + suffix)); err == nil {
			t.Fatalf("accepted %s", suffix)
		}
	}
	for _, input := range []string{valid + `{}`, `{"artifact_id":"../outside"}`, `{"artifact_id":null}`, `{}`} {
		if _, err := DecodeReadArtifactInput([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestArtifactDescriptorStrictRoundTrip(t *testing.T) {
	d := testArtifactDescriptor(t, true)
	b, err := EncodeArtifactDescriptor(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeArtifactDescriptor(b)
	if err != nil || got != d {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, bad := range []string{strings.Replace(string(b), `"saved_bytes":3`, `"saved_bytes":4`, 1), strings.Replace(string(b), `"complete":true`, `"complete":null`, 1), strings.Replace(string(b), `"complete":true,`, "", 1), strings.TrimSuffix(string(b), "}") + `,"extra":0}`, string(b) + `{}`} {
		if bad == string(b) {
			continue
		}
		if _, err := DecodeArtifactDescriptor([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	b[0] = 'x'
	if d != got {
		t.Fatal("encoded bytes alias descriptor")
	}
	for _, id := range []string{"", "../outside", strings.Repeat("A", 32), strings.Repeat("g", 32)} {
		if _, err := ParseArtifactID(id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
}

func TestArtifactSavingConsistency(t *testing.T) {
	complete := testArtifactDescriptor(t, true)
	partial := testArtifactDescriptor(t, false)
	for _, test := range []struct {
		s  ArtifactSaveStatus
		r  string
		d  ArtifactDescriptor
		ok bool
	}{
		{ArtifactNotNeeded, "", ArtifactDescriptor{}, true}, {ArtifactComplete, "", complete, true}, {ArtifactPartial, "stream_limit", partial, true}, {ArtifactUnavailable, "quota_exceeded", ArtifactDescriptor{}, true},
		{ArtifactComplete, "", partial, false}, {ArtifactPartial, "stream_limit", complete, false}, {ArtifactUnavailable, "write_failed", partial, false}, {ArtifactNotNeeded, "", complete, false}, {ArtifactPartial, "host secret", partial, false},
	} {
		_, err := NewArtifactSaving(test.s, test.r, test.d)
		if (err == nil) != test.ok {
			t.Fatalf("state %s: %v", test.s, err)
		}
	}
}

func TestArtifactFrozenCodecsRejectMissingAndUnknownFields(t *testing.T) {
	d := testArtifactDescriptor(t, true)
	s, _ := NewArtifactSaving(ArtifactComplete, "", d)
	b, err := EncodeArtifactSaving(s)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeArtifactSaving(b)
	if err != nil || restored != s {
		t.Fatalf("saving roundtrip: %v", err)
	}
	if _, err = DecodeArtifactSaving([]byte(`{"status":"not_needed","reason":""}`)); err == nil {
		t.Fatal("missing descriptor accepted")
	}
	for _, bad := range []string{`{"status":"not_needed","reason":"","descriptor":null,"extra":1}`, `{"status":"not_needed","reason":null,"descriptor":null}`, `{"status":"complete","reason":"","descriptor":null}`} {
		if _, err = DecodeArtifactSaving([]byte(bad)); err == nil {
			t.Fatal("invalid saving accepted")
		}
	}
	p, _ := NewArtifactPage(d.ID(), 0, 3, 3, true, "abc")
	b, err = EncodeArtifactPage(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeArtifactPage(b)
	if err != nil || got != p {
		t.Fatalf("page roundtrip: %v", err)
	}
	for _, bad := range []string{strings.Replace(string(b), `"offset":0,`, "", 1), strings.Replace(string(b), `"source_complete":true`, `"source_complete":null`, 1), strings.TrimSuffix(string(b), "}") + `,"extra":0}`, string(b) + `{}`} {
		if _, err = DecodeArtifactPage([]byte(bad)); err == nil {
			t.Fatal("invalid page accepted")
		}
	}
}
