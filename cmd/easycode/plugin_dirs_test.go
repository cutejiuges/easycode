package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
)

func TestPluginFlagsRejectPrivateInvalidValuesBeforeResources(t *testing.T) {
	for _, prefix := range [][]string{nil, {"resume"}} {
		for _, value := range []string{"", "/private/" + strings.Repeat("s", 4096), string([]byte{'/', 0xff, 'x'}), "/private/\x00invalid"} {
			for _, suffix := range [][]string{nil, {"--help"}} {
				var output, errors bytes.Buffer
				args := append(append([]string(nil), prefix...), "--plugin-dir", value)
				args = append(args, suffix...)
				code := run(context.Background(), args, strings.NewReader("not read"), &output, &errors, false)
				if code != 2 || output.Len() != 0 || !strings.Contains(errors.String(), "plugin_argument_invalid") {
					t.Fatalf("invalid value: %d %q", code, errors.String())
				}
				if value != "" && (strings.Contains(errors.String(), value) || strings.Contains(errors.String(), fmt.Sprintf("%q", value))) {
					t.Fatal("private invalid argument echoed")
				}
			}
		}
		var args []string
		args = append(args, prefix...)
		for i := 0; i < 33; i++ {
			args = append(args, "--plugin-dir", "/private/configured-path")
		}
		var output bytes.Buffer
		if code := run(context.Background(), args, strings.NewReader(""), &output, &output, false); code != 2 || !strings.Contains(output.String(), "plugin_argument_limit") || strings.Contains(output.String(), "configured-path") {
			t.Fatalf("count: %d %q", code, output.String())
		}
	}
}

func TestPluginFlagsHelpAndCopiesDoNotRevealConfiguredPaths(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	var output bytes.Buffer
	flags.SetOutput(&output)
	dirs := pluginDirectoryFlags(flags)
	if err := flags.Parse([]string{"--plugin-dir", "/private/team-package", "--plugin-dir", "./other"}); err != nil || dirs.err != nil {
		t.Fatal(err, dirs.err)
	}
	values := dirs.Values()
	values[0] = "changed"
	if dirs.Values()[0] != "/private/team-package" {
		t.Fatal("values alias")
	}
	flags.PrintDefaults()
	if strings.Contains(output.String(), "/private/") || strings.Contains(output.String(), "./other") {
		t.Fatal("flag defaults reveal configured values")
	}
	for _, args := range [][]string{{"--plugin-dir", "/private/not-opened", "--help"}, {"resume", "--plugin-dir", "/private/not-opened", "--help"}, {"--plugin-dir", "/private/not-opened", "--version"}} {
		output.Reset()
		if code := run(context.Background(), args, strings.NewReader(""), &output, &output, false); code != 0 || strings.Contains(output.String(), "/private/") {
			t.Fatalf("offline flag: %d %q", code, output.String())
		}
	}
}
