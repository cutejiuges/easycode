package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"easycode/internal/app"
	"easycode/internal/fault"
	"easycode/internal/headless"
)

func runStreamCommand(ctx context.Context, options app.Options, errorOutput io.Writer) int {
	if input, ok := options.Input.(*os.File); ok {
		stream, release, err := openCLIStreamFile(input)
		if err != nil {
			_, _ = fmt.Fprintf(errorOutput, "error: %s: configure stream input failed\n", fault.CodeInputRead)
			return 1
		}
		defer release()
		options.Input = stream
	}
	if output, ok := options.Output.(*os.File); ok {
		stream, release, err := openCLIStreamFile(output)
		if err != nil {
			_, _ = fmt.Fprintf(errorOutput, "error: %s: configure stream output failed\n", fault.CodeOutput)
			return 1
		}
		defer release()
		options.Output = stream
	}

	outcome := app.Run(ctx, options)
	if closer, ok := options.Output.(io.Closer); ok && outcome.Report == app.ReportPending {
		// 运行前的失败和关闭失败也可能需要输出最后一帧。
		closed := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(closed)
			_ = closer.Close()
		})
		defer func() {
			if !stop() {
				<-closed
			}
		}()
	}
	return renderOutcome(outcome, headless.ModeStreamJSON, options.Output, errorOutput)
}
