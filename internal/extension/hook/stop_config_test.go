package hook

import (
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestStopConfigurationIsStrictOrderedAndMatcherFree(t *testing.T) {
	stop := `[{"hooks":[{"id":"stop","type":"command","command":"true"}]}]`
	tool := `[{"matcher":"*","hooks":[{"id":"tool","type":"command","command":"true"}]}]`
	a, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"Stop":`+stop+`,"PreToolUse":`+tool+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"PreToolUse":`+tool+`,"Stop":`+stop+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || len(b) != 2 || a[0] != b[0] || a[1] != b[1] || a[0].Reference.Event != domain.HookPreToolUse || a[1].Reference.Event != domain.HookStop || a[1].Reference.Feedback != domain.HookFeedbackNone {
		t.Fatal("canonical order or disclosure changed")
	}
	if _, ok := a[1].Scope.ToolMatcher(); ok {
		t.Fatal("completion declaration has a tool matcher")
	}
	model, err := decodeConfig(domain.HookSourceUser, []byte(`{"hooks":{"Stop":`+strings.Replace(stop, `"id":"stop"`, `"id":"stop","feedback":"model"`, 1)+`,"PreToolUse":`+tool+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	if model[0].Reference.Revision == a[0].Reference.Revision || model[1].Reference.Fingerprint == a[1].Reference.Fingerprint {
		t.Fatal("disclosure did not invalidate identity")
	}
	for _, config := range []string{
		`{"hooks":{"Stop":[{"matcher":"*","hooks":[{"id":"x","type":"command","command":"true"}]}]}}`,
		`{"hooks":{"Stop":[{"matcher":null,"hooks":[{"id":"x","type":"command","command":"true"}]}]}}`,
		`{"hooks":{"Stop":null}}`, `{"hooks":{"Stop":[{"hooks":[]}]}}`, `{"hooks":{"Stop":[{"hooks":null}]}}`,
		`{"hooks":{"Stop":` + strings.Replace(stop, `"command":"true"`, `"command":"true","feedback":null`, 1) + `}}`,
		`{"hooks":{"Stop":` + strings.Replace(stop, `"command":"true"`, `"command":"true","feedback":"MODEL"`, 1) + `}}`,
		`{"hooks":{"Stop":` + strings.Replace(stop, `"id":"stop"`, `"id":"stop","id":"second"`, 1) + `}}`,
		`{"hooks":{"Stop":` + stop + `,"PreToolUse":` + strings.Replace(tool, `"id":"tool"`, `"id":"stop"`, 1) + `}}`,
		`{"hooks":{"stop":[]}}`, `{"hooks":{"StopFailure":[]}}`, `{"hooks":{"Stop":` + stop + `}} {}`,
	} {
		if _, err := decodeConfig(domain.HookSourceUser, []byte(config)); err == nil {
			t.Fatal("invalid source accepted", config)
		}
	}
	for _, config := range []string{`{"hooks":{}}`, `{"hooks":{"Stop":[]}}`, `{"hooks":{"PreToolUse":` + tool + `}}`} {
		ds, err := decodeConfig(domain.HookSourceUser, []byte(config))
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := domain.NewHookSnapshot(ds, nil)
		if err != nil {
			t.Fatal(err)
		}
		checks, err := snapshot.StopChecks()
		if err != nil || len(checks) != 0 {
			t.Fatal("empty completion path changed", err)
		}
	}
}

func TestStopLimitsIncludeAllFourEventsAndUnavailableSources(t *testing.T) {
	commands := func(start, count int) string {
		var parts []string
		for i := start; i < start+count; i++ {
			parts = append(parts, fmt.Sprintf(`{"id":"check-%d","type":"command","command":"true"}`, i))
		}
		return strings.Join(parts, ",")
	}
	config := func(stops int) string {
		return `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[` + commands(0, 8) + `]}],"PostToolUse":[{"matcher":"*","hooks":[` + commands(8, 8) + `]}],"PostToolUseFailure":[{"matcher":"*","hooks":[` + commands(16, 8) + `]}],"Stop":[{"hooks":[` + commands(24, stops) + `]}]}}`
	}
	ds, err := decodeConfig(domain.HookSourceUser, []byte(config(8)))
	if err != nil || len(ds) != 32 {
		t.Fatal("four-event boundary", err)
	}
	if _, err := decodeConfig(domain.HookSourceUser, []byte(config(9))); err == nil {
		t.Fatal("aggregate source overflow accepted")
	}
	snapshot, err := domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := snapshot.StopChecks()
	if err != nil || len(checks) != 8 {
		t.Fatal("stop boundary", err)
	}
	checks[0].Command = "changed"
	if snapshot.Declarations()[24].Command != "true" {
		t.Fatal("snapshot alias")
	}
	bad, err := domain.NewHookSnapshot(ds, []domain.HookDiagnostic{{Source: domain.HookSourceProject, Code: domain.HookConfigInvalid}})
	if err != nil {
		t.Fatal(err)
	}
	if partial, err := bad.StopChecks(); err != domain.HookConfigInvalid || len(partial) != 0 {
		t.Fatal("partial source allowed completion", err)
	}
	more, err := decodeConfig(domain.HookSourceProject, []byte(`{"hooks":{"Stop":[{"hooks":[`+commands(0, 9)+`]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = domain.NewHookSnapshot(more, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checks, err := snapshot.StopChecks(); err != domain.HookMatchLimit || len(checks) != 0 {
		t.Fatal("over-matched completion accepted", err)
	}
}
