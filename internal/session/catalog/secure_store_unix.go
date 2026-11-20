//go:build darwin || linux

package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type secureStore struct {
	directory *os.File
	lockFile  *os.File
	snapshot  *os.File
	name      string
}

func openSecureStore(ctx context.Context, snapshotPath string) (*secureStore, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := filepath.Base(snapshotPath)
	if name == "." || name == ".." || filepath.IsAbs(name) {
		return nil, fmt.Errorf("catalog snapshot file name is invalid")
	}

	directoryPath := filepath.Dir(snapshotPath)
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		return nil, fmt.Errorf("create catalog data home: %w", err)
	}
	directoryFD, err := unix.Open(directoryPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open catalog data home: %w", err)
	}
	store := &secureStore{directory: os.NewFile(uintptr(directoryFD), "catalog-data-home"), name: name}
	if err := store.checkDirectory(); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	store.lockFile, err = store.openPrivateFile(name+".lock", unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, errors.Join(err, store.Close())
	}
	if err := acquireStoreLock(ctx, store.lockFile); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	if err := store.checkLease(); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func acquireStoreLock(ctx context.Context, file *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("lock session catalog: %w", err)
		}
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
			timer.Stop()
			return fmt.Errorf("lock session catalog: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// openSnapshot 返回由 store 持有的只读句柄。保留句柄直到发布或关闭，防止 inode 被回收复用。
func (store *secureStore) openSnapshot() (*os.File, error) {
	file, err := store.openPrivateFile(store.name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	store.snapshot = file
	return file, nil
}

func (store *secureStore) publishSnapshot(ctx context.Context, write func(io.Writer) error) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.checkLease(); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("create catalog staging name: %w", err)
	}
	stagingName := ".catalog-stage-" + hex.EncodeToString(random[:])
	staged, err := store.openPrivateFile(stagingName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			resultErr = errors.Join(resultErr, store.discardStage(stagingName, staged), staged.Close())
		}
	}()

	if err := write(staged); err != nil {
		return fmt.Errorf("write catalog snapshot: %w", err)
	}
	if err := staged.Sync(); err != nil {
		return fmt.Errorf("sync catalog staging file: %w", err)
	}
	if err := store.checkSnapshot(); err != nil {
		return err
	}
	if err := store.checkEntry(stagingName, staged); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	directoryFD := int(store.directory.Fd())
	if store.snapshot == nil {
		err = renameCatalogNoReplace(directoryFD, stagingName, store.name)
	} else {
		err = unix.Renameat(directoryFD, stagingName, directoryFD, store.name)
	}
	if err != nil {
		return fmt.Errorf("replace session catalog snapshot: %w", err)
	}
	published = true
	previous := store.snapshot
	store.snapshot = staged
	identityErr := store.checkEntry(store.name, staged)
	syncErr := store.directory.Sync()
	if syncErr != nil {
		syncErr = fmt.Errorf("sync catalog data home: %w", syncErr)
	}
	var closeErr error
	if previous != nil {
		closeErr = previous.Close()
	}
	if err := errors.Join(identityErr, syncErr, closeErr); err != nil {
		return errors.Join(errSnapshotPublicationUncertain, err)
	}
	return nil
}

func (store *secureStore) checkDirectory() error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(store.directory.Fd()), &stat); err != nil {
		return fmt.Errorf("inspect catalog data home: %w", err)
	}
	if uint32(stat.Mode)&unix.S_IFMT != unix.S_IFDIR || uint32(stat.Mode)&0o7777 != 0o700 || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("catalog data home permissions are unsafe")
	}
	return nil
}

func (store *secureStore) checkLease() error {
	if err := store.checkDirectory(); err != nil {
		return err
	}
	return store.checkEntry(store.name+".lock", store.lockFile)
}

func (store *secureStore) checkSnapshot() error {
	if err := store.checkLease(); err != nil {
		return err
	}
	if store.snapshot == nil {
		// 首次发布由原子的 no-replace 操作保护。
		return nil
	}
	return store.checkEntry(store.name, store.snapshot)
}

func (store *secureStore) checkEntry(name string, file *os.File) error {
	var entry unix.Stat_t
	if err := unix.Fstatat(int(store.directory.Fd()), name, &entry, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect catalog directory entry: %w", err)
	}
	if !isPrivateCatalogFile(entry) {
		return fmt.Errorf("catalog file identity or permissions changed")
	}
	var held unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &held); err != nil {
		return fmt.Errorf("inspect held catalog file: %w", err)
	}
	if entry.Dev != held.Dev || entry.Ino != held.Ino || !isPrivateCatalogFile(held) {
		return fmt.Errorf("catalog file identity or permissions changed")
	}
	return nil
}

func (store *secureStore) discardStage(name string, file *os.File) error {
	if err := store.checkEntry(name, file); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := unix.Unlinkat(int(store.directory.Fd()), name, 0); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove catalog staging file: %w", err)
	}
	return nil
}

func (store *secureStore) openPrivateFile(name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(store.directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open catalog file: %w", err)
	}
	file := os.NewFile(uintptr(fd), "catalog-file")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, errors.Join(fmt.Errorf("inspect catalog file: %w", err), file.Close())
	}
	if !isPrivateCatalogFile(stat) {
		return nil, errors.Join(fmt.Errorf("catalog file permissions or type are unsafe"), file.Close())
	}
	return file, nil
}

func isPrivateCatalogFile(stat unix.Stat_t) bool {
	return uint32(stat.Mode)&unix.S_IFMT == unix.S_IFREG && uint32(stat.Mode)&0o7777 == 0o600 &&
		stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}

func (store *secureStore) Close() error {
	if store == nil {
		return nil
	}
	var result error
	if store.snapshot != nil {
		result = errors.Join(result, store.snapshot.Close())
		store.snapshot = nil
	}
	if store.lockFile != nil {
		result = errors.Join(result, store.lockFile.Close())
		store.lockFile = nil
	}
	if store.directory != nil {
		result = errors.Join(result, store.directory.Close())
		store.directory = nil
	}
	return result
}
