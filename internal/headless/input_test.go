package headless

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestResolvePromptInputMatrix(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		stdin     string
		terminal  bool
		want      string
		wantKind  InputErrorKind
	}{
		{name: "piped stdin", stdin: "hello", want: "hello"},
		{name: "forced terminal stdin", arguments: []string{"-"}, stdin: "hello", terminal: true, want: "hello"},
		{name: "argument on terminal", arguments: []string{"hello"}, terminal: true, want: "hello"},
		{name: "empty pipe with argument", arguments: []string{"hello"}, want: "hello"},
		{name: "append pipe", arguments: []string{"summarize"}, stdin: "content", want: "summarize\n\n<stdin>\ncontent\n</stdin>"},
		{name: "preserve pipe newline", arguments: []string{"summarize"}, stdin: "content\n", want: "summarize\n\n<stdin>\ncontent\n</stdin>"},
		{name: "missing terminal input", terminal: true, wantKind: InputErrorUsage},
		{name: "multiple arguments", arguments: []string{"one", "two"}, wantKind: InputErrorUsage},
		{name: "empty input", stdin: " \n\t", wantKind: InputErrorUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolvePrompt(test.arguments, strings.NewReader(test.stdin), test.terminal)
			if test.wantKind != 0 {
				var inputError *InputError
				if !errors.As(err, &inputError) || inputError.Kind != test.wantKind {
					t.Fatalf("error = %v, want kind %d", err, test.wantKind)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ResolvePrompt() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestResolvePromptValidatesUTF8AndBounds(t *testing.T) {
	if _, err := ResolvePrompt(nil, bytes.NewReader([]byte{0xff}), false); err == nil {
		t.Fatal("expected invalid UTF-8 error")
	}
	if _, err := ResolvePrompt(nil, strings.NewReader(strings.Repeat("x", MaxPromptBytes+1)), false); err == nil {
		t.Fatal("expected oversized stdin error")
	}
	if _, err := ResolvePrompt([]string{strings.Repeat("x", MaxPromptBytes+1)}, strings.NewReader(""), true); err == nil {
		t.Fatal("expected oversized argument error")
	}

	argument := "summarize"
	allowed := MaxPromptBytes - len(argument) - len(stdinPrefix) - len(stdinSuffix)
	exact := strings.Repeat("x", allowed-1) + "\n"
	got, err := ResolvePrompt([]string{argument}, strings.NewReader(exact), false)
	if err != nil || len(got) != MaxPromptBytes {
		t.Fatalf("exact boundary length/error = %d/%v", len(got), err)
	}
	if _, err := ResolvePrompt([]string{argument}, strings.NewReader(exact+"x"), false); err == nil {
		t.Fatal("expected combined oversized input error")
	}
}

func TestResolvePromptClassifiesReadFailure(t *testing.T) {
	_, err := ResolvePrompt(nil, errorReader{}, false)
	var inputError *InputError
	if !errors.As(err, &inputError) || inputError.Kind != InputErrorRead {
		t.Fatalf("read error = %v", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("secret read failure")
}
