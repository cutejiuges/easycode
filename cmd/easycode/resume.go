package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"easycode/internal/app"
	"easycode/internal/headless"
	"github.com/charmbracelet/x/term"
)

func outputIsTerminal(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}
func runResume(ctx context.Context, args []string, input io.Reader, output, errorOutput io.Writer, stdinTerminal, stdoutTerminal bool) int {
	flags := flag.NewFlagSet("easycode resume", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	all := flags.Bool("all", false, "show sessions from all creation directories")
	configPath := flags.String("config", "", "configuration file path")
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "Usage: easycode resume [--all] [--config PATH]\n\nSelect a session interactively; Ctrl+V opens a read-only text preview.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(flags.Args()) != 0 {
		fmt.Fprintln(errorOutput, "error: resume does not accept positional arguments")
		return 2
	}
	if !stdinTerminal || !stdoutTerminal {
		fmt.Fprintln(errorOutput, "error: resume requires terminal stdin and stdout")
		return 2
	}
	return renderOutcome(app.RunSessionPicker(ctx, app.Options{ConfigPath: *configPath, Input: input, Output: output}, *all), headless.ModeInteractive, output, errorOutput)
}
