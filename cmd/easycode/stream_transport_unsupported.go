//go:build !darwin && !linux

package main

import "os"

func openCLIStreamFile(file *os.File) (*os.File, func(), error) {
	return file, func() {}, nil
}
