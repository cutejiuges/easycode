package codec

import (
	"strings"
	"testing"
)

// BenchmarkStrictRecord 同时观测小对象、嵌套结构和大文本的重复解码成本。
func BenchmarkStrictRecord(b *testing.B) {
	type entry struct {
		Text string `json:"text"`
	}
	type record struct {
		Name    string  `json:"name"`
		Entries []entry `json:"entries"`
	}
	for _, test := range []struct{ name, data string }{
		{"small", `{"name":"x","entries":[{"text":"ok"}]}`},
		{"nested", `{"name":"x","entries":[` + strings.TrimSuffix(strings.Repeat(`{"text":"nested"},`, 64), ",") + `]}`},
		{"large", `{"name":"x","entries":[{"text":"` + strings.Repeat("text", 128<<10) + `"}]}`},
	} {
		b.Run(test.name, func(b *testing.B) {
			data := []byte(test.data)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var value record
				if err := UnmarshalRecord(data, &value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
