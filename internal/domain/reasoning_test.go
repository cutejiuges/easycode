package domain

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/codec"
)

func TestReasoningBodyPreservesPartsAndReadsVirtualRanges(t *testing.T) {
	parts := []string{"", " 中文🙂 ", "", "\nnext\t", " "}
	body, err := NewReasoningBody(ReasoningOpenAISummary, parts)
	if err != nil {
		t.Fatal(err)
	}
	parts[1] = "mutated"
	want := " 中文🙂 \n\n\nnext\t\n\n "
	if body.Len() != uint64(len(want)) || body.Availability() != ReasoningReadable {
		t.Fatal("reasoning size or availability changed")
	}
	for offset := 0; offset <= len(want); offset++ {
		for _, backward := range []bool{false, true} {
			for limit := uint32(1); limit <= 9; limit++ {
				page, err := body.ReadRange(uint64(offset), limit, backward)
				if offset < len(want) && !utf8.RuneStart(want[offset]) {
					if err == nil {
						t.Fatal("accepted an interior UTF-8 offset")
					}
					continue
				}
				if err != nil {
					// 小页容不下一个完整字符或向前读到开头时应明确拒绝。
					if limit >= 4 && !(backward && offset == 0) {
						t.Fatalf("unexpected error at %d/%d/%v: %v", offset, limit, backward, err)
					}
					continue
				}
				if page.TotalBytes != uint64(len(want)) || page.NextOffset-page.Offset > uint64(limit) || !utf8.ValidString(page.Text) || page.Text != want[page.Offset:page.NextOffset] {
					t.Fatalf("incorrect virtual page: %#v", page)
				}
				if backward && page.NextOffset != uint64(offset) || !backward && page.Offset != uint64(offset) {
					t.Fatal("page moved its requested boundary")
				}
			}
		}
	}
	page, err := body.ReadRange(0, 64<<10, false)
	if err != nil || page.Text != want {
		t.Fatalf("full page: %#v %v", page, err)
	}
}

func TestReasoningCurrentEncodingAndUnavailable(t *testing.T) {
	body, _ := NewReasoningBody(ReasoningOpenAISummary, []string{"A", " B\n"})
	data, err := codec.MarshalStable(body)
	const expected = `{"source":"openai_summary","availability":"readable","parts":["A"," B\n"]}`
	if err != nil || string(data) != expected {
		t.Fatalf("unexpected reasoning encoding: %s %v", data, err)
	}
	decoded, err := DecodeReasoningBody(data)
	if err != nil || !reflect.DeepEqual(decoded, body) {
		t.Fatal("reasoning body did not round trip", err)
	}
	for _, invalid := range []string{
		`null`,
		`{}`,
		`{"source":"openai_summary","availability":"readable","parts":null}`,
		`{"source":"openai_summary","availability":"readable","parts":[null]}`,
		`{"source":"openai_summary","availability":"readable","parts":[]}`,
		`{"source":"openai_summary","availability":"unavailable","parts":["A"]}`,
		`{"source":"openai_summary","availability":"readable","parts":["","A"]}`,
		`{"source":"raw","availability":"readable","parts":["A"]}`,
		strings.Replace(expected, `"source":`, `"source":"openai_summary","source":`, 1),
		strings.Replace(expected, `"parts":`, `"signature":"opaque","parts":`, 1),
	} {
		if _, err := DecodeReasoningBody([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid reasoning: %s", invalid)
		}
	}
	unavailable, err := NewReasoningBody(ReasoningAnthropicThinking, []string{"", ""})
	if err != nil || unavailable.Availability() != ReasoningUnavailable || unavailable.Len() != 0 {
		t.Fatal("invalid unavailable body", err)
	}
	if _, err := unavailable.ReadRange(0, 10, false); err == nil {
		t.Fatal("unavailable body became readable")
	}
	if _, err := NewReasoningBody(ReasoningAnthropicThinking, []string{"a", "b"}); err == nil {
		t.Fatal("thinking accepted multiple nonempty parts")
	}
}

func TestTranscriptReasoningIsStrictAndIndependent(t *testing.T) {
	turn := TurnID("01900000-0000-7000-8000-000000000001")
	body, _ := NewReasoningBody(ReasoningAnthropicThinking, []string{"allowed"})
	item, err := NewTranscriptReasoning(TranscriptPosition{}, turn, body)
	if err != nil {
		t.Fatal(err)
	}
	data, err := codec.MarshalStable(item)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTranscriptItem(data)
	if err != nil || !reflect.DeepEqual(decoded, item) {
		t.Fatal("reasoning transcript did not round trip", err)
	}
	for _, invalid := range []string{
		strings.Replace(string(data), `"reasoning":`, `"reasoning":null,"unknown":`, 1),
		strings.Replace(string(data), `"kind":"reasoning"`, `"kind":"assistant"`, 1),
		strings.Replace(string(data), `"text":""`, `"text":"mixed"`, 1),
		strings.Replace(string(data), `"source":"anthropic_thinking"`, `"source":"raw"`, 1),
		strings.Replace(string(data), `"parts":`, `"encrypted_content":"opaque","parts":`, 1),
	} {
		if _, err := DecodeTranscriptItem([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid transcript: %s", invalid)
		}
	}
	view, err := NewTranscriptView(ProviderAnthropic, []TranscriptItem{item})
	if err != nil {
		t.Fatal(err)
	}
	*view.Items[0].Reasoning = ReasoningBody{}
	if item.Reasoning.Validate() != nil {
		t.Fatal("returned view shares a mutable reasoning pointer")
	}
}

func TestReasoningLimitsBeforeCopyAndSmallPageAllocation(t *testing.T) {
	if _, err := NewReasoningBody(ReasoningOpenAISummary, make([]string, MaxReasoningNodes+1)); err == nil {
		t.Fatal("accepted excess parts")
	}
	if _, err := NewReasoningBody(ReasoningOpenAISummary, []string{strings.Repeat("x", MaxReasoningSourceBytes), "y"}); err == nil {
		t.Fatal("accepted excess joined bytes")
	}
	body, err := NewReasoningBody(ReasoningOpenAISummary, []string{strings.Repeat("x", 1<<20), "end"})
	if err != nil {
		t.Fatal(err)
	}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			page, err := body.ReadRange(body.Len()-3, 3, false)
			if err != nil || page.Text != "end" {
				b.Fatal("incorrect final page", err)
			}
		}
	})
	if result.AllocedBytesPerOp() > 256 {
		t.Fatalf("small range copied a large body: %d bytes/op", result.AllocedBytesPerOp())
	}
}
