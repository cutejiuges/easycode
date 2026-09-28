package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"easycode/internal/domain"
)

// Repository 将 thread UUIDv7 安全映射到受限数据根内的 journal。
type Repository struct {
	rootPath string
	root     *os.Root
}

// NewRepository 打开或创建用户私有 Session 数据根。
func NewRepository(rootPath string) (*Repository, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("session data root is required")
	}
	absolute, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve session data root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if err := ensureDataRoot(absolute); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("open session data root: %w", err)
	}
	return &Repository{rootPath: absolute, root: root}, nil
}

// RootPath 返回装配时固定的绝对数据根，仅供索引和诊断使用。
func (repository *Repository) RootPath() string {
	if repository == nil {
		return ""
	}
	return repository.rootPath
}

// JournalPath 返回 thread journal 的绝对定位结果，但不打开文件。
func (repository *Repository) JournalPath(threadID domain.ThreadID) (string, error) {
	relative, err := journalRelativePath(threadID)
	if err != nil {
		return "", err
	}
	return filepath.Join(repository.rootPath, filepath.FromSlash(relative)), nil
}

// Create 创建新的私有 thread journal，取得独占 lease，并同步其父目录项。
func (repository *Repository) Create(ctx context.Context, threadID domain.ThreadID) (*JournalLease, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	relative, err := journalRelativePath(threadID)
	if err != nil {
		return nil, err
	}
	directory := path.Dir(relative)
	if err := repository.ensurePrivateDirectories(directory); err != nil {
		return nil, err
	}
	file, err := repository.root.OpenFile(relative, os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create session journal: %w", err)
	}
	if err := validatePrivateFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	lease, err := acquireJournalLease(file, threadID)
	if err != nil {
		return nil, err
	}
	parent, err := repository.root.Open(directory)
	if err != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("open session journal directory: %w", err)
	}
	syncErr := parent.Sync()
	closeErr := parent.Close()
	if syncErr != nil || closeErr != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("sync session journal directory: %w", errors.Join(syncErr, closeErr))
	}
	return lease, nil
}

// Open 打开已有私有普通 journal，并在读取或修复前取得独占 lease。
func (repository *Repository) Open(ctx context.Context, threadID domain.ThreadID) (*JournalLease, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	relative, err := journalRelativePath(threadID)
	if err != nil {
		return nil, err
	}
	if err := repository.validateExistingPath(relative); err != nil {
		return nil, err
	}
	file, err := repository.root.OpenFile(relative, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, fmt.Errorf("open session journal: %w", err)
	}
	if err := validatePrivateFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return acquireJournalLease(file, threadID)
}

func acquireJournalLease(file *os.File, threadID domain.ThreadID) (*JournalLease, error) {
	if err := tryLockJournal(file); err != nil {
		_ = file.Close()
		if IsJournalBusy(err) {
			return nil, errJournalBusy
		}
		return nil, fmt.Errorf("lock session journal: %w", err)
	}
	return newJournalLease(file, threadID), nil
}

// Close 释放受限 filesystem root。
func (repository *Repository) Close() error {
	if repository == nil || repository.root == nil {
		return nil
	}
	return repository.root.Close()
}

func ensureDataRoot(rootPath string) error {
	info, err := os.Lstat(rootPath)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(rootPath, 0o700); err != nil {
			return fmt.Errorf("create session data root: %w", err)
		}
		info, err = os.Lstat(rootPath)
	}
	if err != nil {
		return fmt.Errorf("inspect session data root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("session data root must be a real directory")
	}
	if err := validatePrivatePermissions(info, 0o700, "session data root"); err != nil {
		return err
	}
	return nil
}

func (repository *Repository) ensurePrivateDirectories(relative string) error {
	current := ""
	for _, component := range splitPath(relative) {
		if current == "" {
			current = component
		} else {
			current = path.Join(current, component)
		}
		if err := repository.ensurePrivateDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func (repository *Repository) ensurePrivateDirectory(relative string) error {
	info, err := repository.root.Lstat(relative)
	if errors.Is(err, fs.ErrNotExist) {
		if err := repository.root.Mkdir(relative, 0o700); err != nil {
			return fmt.Errorf("create session directory: %w", err)
		}
		info, err = repository.root.Lstat(relative)
	}
	if err != nil {
		return fmt.Errorf("inspect session directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("session directory must be a real directory")
	}
	return validatePrivatePermissions(info, 0o700, "session directory")
}

func (repository *Repository) validateExistingPath(relative string) error {
	directory := path.Dir(relative)
	current := ""
	for _, component := range splitPath(directory) {
		if current == "" {
			current = component
		} else {
			current = path.Join(current, component)
		}
		info, err := repository.root.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect session directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("session directory must be a real directory")
		}
		if err := validatePrivatePermissions(info, 0o700, "session directory"); err != nil {
			return err
		}
	}
	info, err := repository.root.Lstat(relative)
	if err != nil {
		return fmt.Errorf("inspect session journal: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("session journal must be a regular file")
	}
	return validatePrivatePermissions(info, 0o600, "session journal")
}

func validatePrivateFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect session journal: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("session journal must be a regular file")
	}
	return validatePrivatePermissions(info, 0o600, "session journal")
}

func validatePrivatePermissions(info fs.FileInfo, want fs.FileMode, label string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("%s permissions are unsafe", label)
	}
	return nil
}

func journalRelativePath(threadID domain.ThreadID) (string, error) {
	parsed, err := domain.ParseThreadID(string(threadID))
	if err != nil {
		return "", fmt.Errorf("invalid session thread ID: %w", err)
	}
	timestamp, err := parsed.Time()
	if err != nil {
		return "", fmt.Errorf("read session thread timestamp: %w", err)
	}
	return path.Join(
		timestamp.Format("2006"), timestamp.Format("01"), timestamp.Format("02"),
		string(parsed)+".jsonl",
	), nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("session context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("session operation cancelled: %w", err)
	}
	return nil
}

func splitPath(value string) []string {
	if value == "" || value == "." {
		return nil
	}
	parts := make([]string, 0, 3)
	for value != "." && value != "/" && value != "" {
		directory, base := path.Split(value)
		parts = append([]string{base}, parts...)
		value = path.Clean(directory)
	}
	return parts
}
