package main

import (
	"context"
	"flag"
	"io"

	"easycode/internal/app"
	"easycode/internal/headless"
	"easycode/internal/session/catalog"
)

const sessionsHelp = `Usage: easycode sessions list [--all] [--limit N]

List local session metadata without model configuration or network access.
  --all       show sessions across all directories (default: current directory)
  --limit N   maximum entries, from 1 to 200 (default: 20)

Refresh scans all stored journals and may rebuild the index or repair incomplete tails under an exclusive lease.
Resume with: easycode --resume <thread-id>
`

func runSessions(ctx context.Context, args []string, output, errorOutput io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return writeSessionsHelp(output)
	}
	if len(args) == 0 || args[0] != "list" {
		return sessionsUsageError(errorOutput)
	}
	args = args[1:]
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return writeSessionsHelp(output)
	}
	flags := flag.NewFlagSet("sessions list", flag.ContinueOnError)
	// 解析器可能回显任意参数，外部只报告固定英文诊断。
	flags.SetOutput(io.Discard)
	all := flags.Bool("all", false, "")
	limit := flags.Int("limit", catalog.DefaultListLimit, "")
	if err := flags.Parse(args); err != nil {
		return sessionsUsageError(errorOutput)
	}
	if len(flags.Args()) != 0 || *limit < 1 || *limit > catalog.MaximumListLimit {
		return sessionsUsageError(errorOutput)
	}
	return renderOutcome(app.ListSessions(ctx, app.SessionListOptions{All: *all, Limit: *limit, Output: output, ErrorOutput: errorOutput}), headless.ModeInteractive, output, errorOutput)
}

func writeSessionsHelp(output io.Writer) int {
	n, err := io.WriteString(output, sessionsHelp)
	if err != nil || n != len(sessionsHelp) {
		return 1
	}
	return 0
}

func sessionsUsageError(output io.Writer) int {
	const message = "error: invalid_input: expected sessions list [--all] [--limit N], with N between 1 and 200\n"
	n, err := io.WriteString(output, message)
	if err != nil || n != len(message) {
		return 1
	}
	return 2
}
