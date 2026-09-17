package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"easycode/internal/app"
)

func main() {
	// 信号上下文统一负责交互界面和后台任务的优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	showVersion := flag.Bool("version", false, "print version and exit")
	headless := flag.Bool("print", false, "run in non-interactive scaffold mode")
	flag.Parse()

	err := app.Run(ctx, app.Options{
		ShowVersion: *showVersion,
		Headless:    *headless,
		Input:       os.Stdin,
		Output:      os.Stdout,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
