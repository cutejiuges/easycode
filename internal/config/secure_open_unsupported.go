//go:build !darwin && !linux

package config

import (
	"fmt"
	"os"
)

func openConfigFileNoFollow(string) (*os.File, error) {
	return nil, fmt.Errorf("secure configuration file opening is unsupported on this platform")
}
