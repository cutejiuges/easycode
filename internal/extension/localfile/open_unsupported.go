//go:build !darwin && !linux

package localfile

import (
	"context"
	"os"
)

func Open(context.Context, string, bool, Options) (*os.File, error) { return nil, Unsupported }
