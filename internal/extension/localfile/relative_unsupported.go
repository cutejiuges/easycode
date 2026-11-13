//go:build !darwin && !linux

package localfile

import (
	"context"
	"os"
)

func FileIdentity(*os.File) (Identity, error)    { return Identity{}, Unsupported }
func InspectEntry(_ *os.File, name string) Entry { return Entry{Name: name, Unsafe: true} }
func OpenRelative(context.Context, *os.File, string, bool, Options) (*os.File, error) {
	return nil, Unsupported
}
