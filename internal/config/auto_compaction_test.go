package config

import (
	"strings"
	"testing"
)

func TestAutoCompactionConfiguration(t *testing.T) {
	for _, test := range []struct {
		fields    string
		enabled   bool
		threshold uint64
	}{
		{`"model":"test"`, false, 0},
		{`"context_window_tokens":10000`, true, 4723},
		{`"context_window_tokens":10000,"auto_compact":false`, false, 0},
		{`"context_window_tokens":10000,"auto_compact":true`, true, 4723},
		{`"auto_compact":false`, false, 0},
		{`"context_window_tokens":2048`, false, 0},
	} {
		decoded, err := decodeFile([]byte("{" + test.fields + "}"))
		if err != nil {
			t.Fatal(err)
		}
		policy, err := decoded.toConfig().AutoCompactionPolicy()
		if err != nil {
			t.Fatal(err)
		}
		threshold, enabled := policy.Threshold()
		if enabled != test.enabled || threshold != test.threshold {
			t.Fatalf("%s: threshold=%d enabled=%v", test.fields, threshold, enabled)
		}
	}
	for _, fields := range []string{`"auto_compact":true`, `"auto_compact":null`, `"auto_compact":1`, `"auto_compact":"private-value"`, `"auto_compact":{}`, `"auto_compact":false,"auto_compact":false`} {
		_, err := decodeFile([]byte("{" + fields + "}"))
		if err == nil {
			t.Fatalf("accepted %s", fields)
		}
		if strings.Contains(err.Error(), "private-value") {
			t.Fatal("configuration value leaked")
		}
	}
}
