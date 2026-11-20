//go:build !darwin && !linux

package catalog

import (
	"context"
	"errors"
	"io"
	"os"
)

var errUnsupportedCatalogStore = errors.New("secure session catalog is unsupported on this platform")

type secureStore struct{}

func openSecureStore(context.Context, string) (*secureStore, error) {
	return nil, errUnsupportedCatalogStore
}

func (*secureStore) openSnapshot() (*os.File, error) {
	return nil, errUnsupportedCatalogStore
}

func (*secureStore) publishSnapshot(context.Context, func(io.Writer) error) error {
	return errUnsupportedCatalogStore
}

func (*secureStore) checkSnapshot() error { return errUnsupportedCatalogStore }

func (*secureStore) Close() error { return nil }
