//go:build !darwin && !linux

package catalog

import (
	"context"
	"fmt"
	"os"
)

type secureStore struct {
	dbName string
}

func openSecureStore(context.Context, string) (*secureStore, error) {
	return nil, fmt.Errorf("secure session catalog is unsupported on this platform")
}

func (*secureStore) ensureDatabase() error {
	return fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) ensureRollbackJournal() error {
	return fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) openTemporaryJournal(string) error {
	return fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) openNamedDatabase(string) (*os.File, error) {
	return nil, fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) validateSidecars() error {
	return fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) createTemporaryDatabase() (string, error) {
	return "", fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) replaceDatabase(string) error {
	return fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) removeTemporary(string) error {
	return fmt.Errorf("secure session catalog is unsupported on this platform")
}
func (*secureStore) path(string) string { return "" }
func (*secureStore) Close() error       { return nil }
