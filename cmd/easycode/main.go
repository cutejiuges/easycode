package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"

	"easycode/internal/app"
	"easycode/internal/config"
	"easycode/internal/fault"
	"easycode/internal/headless"
)

func main() {
	// 信号上下文统一负责交互界面和后台任务的优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, term.IsTerminal(os.Stdin.Fd())))
}

func run(
	ctx context.Context,
	arguments []string,
	input io.Reader,
	output io.Writer,
	errorOutput io.Writer,
	stdinIsTerminal bool,
) int {
	flags := flag.NewFlagSet("easycode", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	showVersion := flags.Bool("version", false, "print version and exit")
	printMode := flags.Bool("print", false, "print only the final assistant text")
	jsonMode := flags.Bool("json", false, "stream versioned JSONL events")
	resumeThreadID := flags.String("resume", "", "resume an existing root thread selected by --resume UUIDv7")
	continueSession := flags.Bool("continue", false, "continue the latest compatible session in the current directory")
	configPath := flags.String(
		"config",
		"",
		"configuration file selected by --config (default: "+config.DefaultPathDisplay+")",
	)
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	mode := headless.ModeInteractive
	if *showVersion {
		return renderOutcome(
			app.Run(ctx, app.Options{ShowVersion: true, Output: output}),
			headless.ModeInteractive, output, errorOutput,
		)
	}
	if *printMode && *jsonMode {
		_, _ = fmt.Fprintln(errorOutput, "error: --print and --json are mutually exclusive")
		return 2
	}
	if *continueSession && *resumeThreadID != "" {
		_, _ = fmt.Fprintln(errorOutput, "error: --resume and --continue are mutually exclusive")
		return 2
	}
	if *printMode {
		mode = headless.ModeText
	} else if *jsonMode {
		mode = headless.ModeJSON
	}

	prompt := ""
	if mode != headless.ModeInteractive {
		resolved, err := headless.ResolvePrompt(flags.Args(), input, stdinIsTerminal)
		if err != nil {
			var inputError *headless.InputError
			if errors.As(err, &inputError) && inputError.Kind == headless.InputErrorUsage {
				_, _ = fmt.Fprintf(errorOutput, "error: %s: %s\n", fault.CodeInvalidInput, err)
				return 2
			}
			_, _ = fmt.Fprintf(errorOutput, "error: %s: %s\n", fault.CodeInputRead, err)
			return 1
		}
		prompt = resolved
	}

	return renderOutcome(app.Run(ctx, app.Options{
		ShowVersion:     *showVersion,
		Mode:            mode,
		Prompt:          prompt,
		ConfigPath:      *configPath,
		ResumeThreadID:  *resumeThreadID,
		ContinueSession: *continueSession,
		Input:           input,
		Output:          output,
	}), mode, output, errorOutput)
}

func renderOutcome(outcome app.Outcome, mode headless.Mode, output io.Writer, errorOutput io.Writer) int {
	if outcome.Class == app.ExitSuccess {
		return 0
	}
	if outcome.Report == app.ReportPending && mode == headless.ModeJSON {
		if err := headless.WriteError(output, outcome.Failure); err == nil {
			return outcome.ExitCode()
		}
		outcome.Report = app.ReportOutputUnavailable
		outcome.Failure = fault.Summary{Code: fault.CodeOutput, Message: "write headless output failed"}
	}
	if outcome.Report == app.ReportPending || outcome.Report == app.ReportOutputUnavailable {
		_, _ = fmt.Fprintf(
			errorOutput, "error: %s: %s\n", outcome.Failure.Code, outcome.Failure.Message,
		)
	}
	return outcome.ExitCode()
}
