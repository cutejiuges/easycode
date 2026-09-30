//go:build !darwin && !linux

package session

import (
	"context"
	"fmt"
	"io/fs"
	"os"
)

type secureRoot struct{}

func openOrCreateSecureRoot(string, securePathHooks) (*secureRoot, error) {
	return nil, fmt.Errorf("secure session paths are unsupported on this platform")
}

func (*secureRoot) openDirectory(string, bool) (*os.File, error) {
	return nil, fmt.Errorf("secure session paths are unsupported on this platform")
}

func (*secureRoot) openJournal(*os.File, string, int, fs.FileMode) (*os.File, error) {
	return nil, fmt.Errorf("secure session paths are unsupported on this platform")
}

func (*secureRoot) enumerateJournals(context.Context) ([]JournalLocation, error) {
	return nil, fmt.Errorf("secure session enumeration is unsupported on this platform")
}

func (*secureRoot) Close() error { return nil }
