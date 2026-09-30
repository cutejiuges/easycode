//go:build darwin || linux

package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type secureStore struct {
	directory *os.File
	lockFile  *os.File
	dbName    string
}

func openSecureStore(ctx context.Context, databasePath string) (*secureStore, error) {
	directoryPath := filepath.Dir(databasePath)
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		return nil, fmt.Errorf("create catalog data home: %w", err)
	}
	directoryFD, err := unix.Open(directoryPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open catalog data home: %w", err)
	}
	directory := os.NewFile(uintptr(directoryFD), directoryPath)
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		_ = directory.Close()
		return nil, fmt.Errorf("catalog data home permissions are unsafe")
	}
	store := &secureStore{directory: directory, dbName: filepath.Base(databasePath)}
	lockFile, err := store.openPrivateFile(store.dbName+".lock", true, false)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := acquireStoreLock(ctx, lockFile); err != nil {
		_ = lockFile.Close()
		_ = store.Close()
		return nil, err
	}
	store.lockFile = lockFile
	return store, nil
}

func acquireStoreLock(ctx context.Context, file *os.File) error {
	if ctx == nil {
		return fmt.Errorf("catalog context is required")
	}
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return fmt.Errorf("lock session catalog: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("lock session catalog: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (store *secureStore) ensureDatabase() error {
	file, err := store.openPrivateFile(store.dbName, true, false)
	if err != nil {
		return err
	}
	return file.Close()
}

func (store *secureStore) ensureRollbackJournal() error {
	file, err := store.openPrivateFile(store.dbName+"-journal", true, false)
	if err != nil {
		return err
	}
	return file.Close()
}

func (store *secureStore) openTemporaryJournal(databaseName string) error {
	file, err := store.openPrivateFile(databaseName+"-journal", true, false)
	if err != nil {
		return err
	}
	return file.Close()
}

func (store *secureStore) openNamedDatabase(name string) (*os.File, error) {
	return store.openPrivateFile(name, false, false)
}

func (store *secureStore) validateSidecars() error {
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		file, err := store.openPrivateFile(store.dbName+suffix, false, true)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close catalog sidecar: %w", closeErr)
		}
	}
	return nil
}

func (store *secureStore) createTemporaryDatabase() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("create catalog rebuild name: %w", err)
	}
	name := "." + store.dbName + ".rebuild-" + hex.EncodeToString(random[:])
	file, err := store.openPrivateFile(name, true, false)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close catalog rebuild database: %w", err)
	}
	return name, nil
}

func (store *secureStore) replaceDatabase(temporaryName string) error {
	if err := unix.Renameat(int(store.directory.Fd()), temporaryName, int(store.directory.Fd()), store.dbName); err != nil {
		return fmt.Errorf("replace session catalog: %w", err)
	}
	if err := store.directory.Sync(); err != nil {
		return fmt.Errorf("sync catalog data home: %w", err)
	}
	return nil
}

func (store *secureStore) removeTemporary(name string) error {
	if name == "" || filepath.Base(name) != name {
		return fmt.Errorf("catalog temporary name is invalid")
	}
	if err := unix.Unlinkat(int(store.directory.Fd()), name, 0); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove catalog temporary database: %w", err)
	}
	return nil
}

func (store *secureStore) path(name string) string {
	return filepath.Join(store.directory.Name(), name)
}

func (store *secureStore) openPrivateFile(name string, create bool, optional bool) (*os.File, error) {
	if store == nil || store.directory == nil || name == "" || filepath.Base(name) != name {
		return nil, fmt.Errorf("catalog file name is invalid")
	}
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Openat(int(store.directory.Fd()), name, flags, 0o600)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return nil, fs.ErrNotExist
		}
		return nil, fmt.Errorf("open catalog file: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, fmt.Errorf("catalog file permissions are unsafe")
	}
	return file, nil
}

func (store *secureStore) Close() error {
	if store == nil {
		return nil
	}
	var result error
	if store.lockFile != nil {
		result = errors.Join(result, unix.Flock(int(store.lockFile.Fd()), unix.LOCK_UN), store.lockFile.Close())
		store.lockFile = nil
	}
	if store.directory != nil {
		result = errors.Join(result, store.directory.Close())
		store.directory = nil
	}
	return result
}
