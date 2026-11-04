package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestResumeCLIValidation(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		in, out bool
		code    int
	}{
		{[]string{"--help"}, false, false, 0}, {[]string{"--bogus"}, true, true, 2}, {[]string{"id"}, true, true, 2}, {nil, false, true, 2}, {nil, true, false, 2}, {[]string{"--config", "/nonexistent/easycode.json"}, true, true, 1},
	} {
		var output, diagnostic bytes.Buffer
		if got := runResume(context.Background(), tc.args, strings.NewReader(""), &output, &diagnostic, tc.in, tc.out); got != tc.code {
			t.Fatalf("%v got %d want %d: %s", tc.args, got, tc.code, diagnostic.String())
		}
	}
}

func TestHeadlessResumePromptKeepsOriginalRoute(t *testing.T) {
	var output, diagnostic bytes.Buffer
	code := run(context.Background(), []string{"--config", "/nonexistent/easycode-picker.json", "--print", "resume"}, strings.NewReader(""), &output, &diagnostic, false)
	if code != 1 || strings.Contains(diagnostic.String(), "requires terminal") {
		t.Fatalf("headless prompt intercepted: %d %s", code, diagnostic.String())
	}
}
func TestSessionsHelpPromisesNoRepair(t *testing.T) {
	var output, diagnostic bytes.Buffer
	if code := runSessions(context.Background(), []string{"--help"}, &output, &diagnostic); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(diagnostic.String()+output.String(), "never repairs journals") {
		t.Fatal("help lost read-only discovery boundary")
	}
}
