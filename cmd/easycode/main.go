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

	"easycode/internal/app"
	"easycode/internal/config"
)

func main() {
	// 信号上下文统一负责交互界面和后台任务的优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(
	ctx context.Context,
	arguments []string,
	input io.Reader,
	output io.Writer,
	errorOutput io.Writer,
) int {
	flags := flag.NewFlagSet("easycode", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	showVersion := flags.Bool("version", false, "print version and exit")
	headless := flags.Bool("print", false, "print mode (not implemented yet)")
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

	err := app.Run(ctx, app.Options{
		ShowVersion: *showVersion,
		Headless:    *headless,
		ConfigPath:  *configPath,
		Input:       input,
		Output:      output,
	})
	if err != nil {
		_, _ = fmt.Fprintf(errorOutput, "error: %s\n", err)
		return 1
	}
	return 0
}
