package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkHighlight(b *testing.B) {
	for _, language := range []highlightLanguage{highlightGo, highlightJSON, highlightPython, highlightJS, highlightTS, highlightShell} {
		for _, size := range []int{1024, 8192, 32768, 65536} {
			b.Run(fmt.Sprintf("language%d/bytes%d", language, size), func(b *testing.B) {
				patterns := map[highlightLanguage]string{highlightGo: "var value = 123 // comment\n", highlightJSON: `{"value":123,"enabled":true},`, highlightPython: "value = f'number {123}' # note\n", highlightJS: "const value = `number ${123}`;\n", highlightTS: "let value: number = 123;\n", highlightShell: "echo \"value $HOME\" # note\n"}
				pattern := patterns[language]
				source := strings.Repeat(pattern, (size-2)/len(pattern))
				if language == highlightJSON {
					source = "[" + strings.TrimSuffix(source, ",") + "]"
				}
				source += strings.Repeat(" ", size-len(source))
				b.ReportAllocs()
				b.SetBytes(int64(len(source)))
				var result highlightResult
				for b.Loop() {
					result = scanHighlight(source, language, testHighlightLimits(len(source)))
					if result.failure != highlightOK {
						b.Fatalf("typical input failed: %v", result.failure)
					}
				}
				b.ReportMetric(float64(result.steps), "steps/op")
				b.ReportMetric(float64(len(result.tokens)), "tokens/op")
				b.ReportMetric(float64(result.peak), "reserved-B/op")
			})
		}
	}
}
func BenchmarkMarkdownSmallDeltas(b *testing.B) {
	for _, step := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("step%d", step), func(b *testing.B) {
			source := "```go\n" + strings.Repeat("var value = 123\n", 4000) + "```\n"
			b.ReportAllocs()
			b.SetBytes(int64(len(source)))
			for b.Loop() {
				p := newMarkdownParser()
				stream := markdownStream{}
				for end := step; end < len(source); end += step {
					stream.advance(&p, source[:end], false, markdownNodeLimit)
				}
				stream.advance(&p, source, true, markdownNodeLimit)
				if p.scanned > len(source)*3 {
					b.Fatalf("excess scan work: %d", p.scanned)
				}
			}
		})
	}
}

func BenchmarkHighlightAdversarial(b *testing.B) {
	for _, sample := range []struct {
		name   string
		lang   highlightLanguage
		source string
		fail   bool
	}{
		{"dense", highlightGo, strings.Repeat("x;", 32768), true},
		{"deep", highlightJS, "`x${" + strings.Repeat("(", 65) + "x", true},
		{"unterminated", highlightPython, "r'''" + strings.Repeat("\\x", 32760), false},
		{"terminator", highlightShell, "cat <<" + strings.Repeat("X", 256) + "\n" + strings.Repeat(strings.Repeat("X", 255)+"Y\n", 240), false},
	} {
		b.Run(sample.name, func(b *testing.B) {
			var result highlightResult
			b.ReportAllocs()
			b.SetBytes(int64(len(sample.source)))
			for b.Loop() {
				result = scanHighlight(sample.source, sample.lang, testHighlightLimits(len(sample.source)))
				if (result.failure == highlightBudgetExceeded) != sample.fail || result.steps > 128*(len(sample.source)+1) {
					b.Fatal("unexpected scan result")
				}
			}
			b.ReportMetric(float64(result.steps), "steps/op")
			b.ReportMetric(float64(result.peak), "reserved-B/op")
			b.ReportMetric(float64(result.failure), "failure/op")
		})
	}
}
func BenchmarkMarkdownDocuments(b *testing.B) {
	for _, sample := range []struct{ name, source string }{{"single-line", strings.Repeat("x", 65536)}, {"table", "| name | path |\n|---|---|\n" + strings.Repeat("| 中文 | /workspace/example/path |\n", 1700)}, {"controls", strings.Repeat("\x1b", 65536)}} {
		b.Run(sample.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(sample.source)))
			for b.Loop() {
				cache := newMarkdownViewCache()
				cache.color = true
				cache.prepare(markdownViewKey{local: 1}, sample.source, 78, true)
				cost := cache.cost()
				if cost.bytes > markdownMemoryLimit || cost.nodes > markdownNodeLimit {
					b.Fatal("derived budget exceeded")
				}
				b.ReportMetric(float64(cost.bytes), "retained-B/op")
				b.ReportMetric(float64(cost.nodes), "retained-nodes/op")
			}
		})
	}
	b.Run("fifty-hot", func(b *testing.B) {
		model := NewModel(context.Background(), "bench", nil)
		for i := 0; i < 50; i++ {
			model.transcript = append(model.transcript, transcriptMessage{local: uint64(i + 1), role: "Assistant", text: "# completed answer"})
		}
		model.prepareMarkdownView()
		parses := model.markdown.parser.parses
		b.ReportAllocs()
		for b.Loop() {
			model.prepareMarkdownView()
			_ = model.View()
		}
		if model.markdown.parser.parses != parses {
			b.Fatal("reparsed hot history")
		}
	})
}

// 同时记录完整流式输入的解析工作量与收口时首次高亮的预留峰值。
func BenchmarkMarkdownColdStreamCompletion(b *testing.B) {
	const size = 64 << 10
	const opening = "```go\n"
	const closing = "\n```\n"
	pattern := "var value = 123\n"
	body := strings.Repeat(pattern, (size-len(opening)-len(closing))/len(pattern))
	source := opening + body + strings.Repeat(" ", size-len(opening)-len(closing)-len(body)) + closing
	for _, step := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("step%d", step), func(b *testing.B) {
			b.SetBytes(size)
			b.ReportAllocs()
			for b.Loop() {
				p := newMarkdownParser()
				var stream markdownStream
				for end := step; end < len(source); end += step {
					stream.advance(&p, source[:end], false, markdownNodeLimit)
				}
				stream.advance(&p, source, true, markdownNodeLimit)
				result := stream.layout(78, true)
				if result.failure || stream.tail.codes == nil || len(stream.tail.codes.items) != 1 {
					b.Fatal("expected one highlighted fence")
				}
				tokens := stream.tail.codes.items[0].result
				if tokens.failure != highlightOK {
					b.Fatal("typical Go block exceeded budget")
				}
				b.ReportMetric(float64(p.scanned), "parsed-B/op")
				b.ReportMetric(float64(tokens.steps), "steps/op")
				b.ReportMetric(float64(tokens.peak), "reserved-B/op")
				b.ReportMetric(float64(len(tokens.tokens)), "tokens/op")
			}
		})
	}
}
