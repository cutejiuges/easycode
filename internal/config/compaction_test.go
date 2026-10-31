package config

import (
	"strings"
	"testing"

	"easycode/internal/fault"
)

func TestCompactionOutputConfiguration(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("EASYCODE_COMPACTION_OUTPUT_TOKENS", "1")
	for _, test := range []struct {
		name, fields string
		want         uint64
	}{
		{"default", "", 4096},
		{"explicit", `,"compaction_output_tokens":8192`, 8192},
		{"minimum", `,"compaction_output_tokens":1`, 1},
		{"maximum", `,"compaction_output_tokens":65536`, 65536},
		{"small window", `,"context_window_tokens":2048,"reserved_output_tokens":512`, 4096},
		{"explicit window", `,"context_window_tokens":8192,"context_safety_margin_tokens":100,"compaction_output_tokens":8091`, 8091},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := strings.TrimSuffix(validFile, "\n}") + test.fields + "}"
			loaded, err := Load(writeConfig(t, t.TempDir(), content, 0o600))
			if err != nil {
				t.Fatal(err)
			}
			if got := loaded.CompactionOutputTokens(); got != test.want {
				t.Fatalf("output = %d, want %d", got, test.want)
			}
			if test.name == "small window" && loaded.ContextBudget.ReservedOutput() != 512 {
				t.Fatal("ordinary reserve changed")
			}
		})
	}
	if got := LoadFromEnv().CompactionOutputTokens(); got != 4096 {
		t.Fatalf("environment changed default: %d", got)
	}
}

func TestRejectInvalidCompactionOutput(t *testing.T) {
	for _, fields := range []string{
		`"compaction_output_tokens":null`, `"compaction_output_tokens":0`,
		`"compaction_output_tokens":-1`, `"compaction_output_tokens":65537`,
		`"compaction_output_tokens":18446744073709551616`, `"compaction_output_tokens":1.5`,
		`"compaction_output_tokens":"private-value"`, `"compaction_output_tokens":1e3`,
		`"compaction_output_tokens":1,"compaction_output_tokens":2`,
		`"context_window_tokens":4096,"compaction_output_tokens":4096`,
		`"context_window_tokens":4096,"context_safety_margin_tokens":100,"compaction_output_tokens":3996`,
	} {
		_, err := decodeFile([]byte("{" + fields + "}"))
		if err == nil {
			t.Fatalf("accepted %s", fields)
		}
		if !strings.Contains(err.Error(), string(fault.CodeInvalidConfiguration)) {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(err.Error(), "private-value") {
			t.Fatal("error leaked value")
		}
	}
}
