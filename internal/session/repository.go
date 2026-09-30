package session

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"

	"easycode/internal/domain"
)

// Repository 将 thread UUIDv7 安全映射到受限数据根内的 journal。
type Repository struct {
	rootPath string
	root     *secureRoot
	hooks    securePathHooks
}

// JournalLocation 是通过安全枚举验证的 canonical journal 定位。
type JournalLocation struct {
	ThreadID     domain.ThreadID
	RelativePath string
}

type securePathHooks struct {
	beforeOpenRoot      func(string)
	afterOpenRoot       func(string)
	beforeOpenComponent func(string)
	afterOpenComponent  func(string)
	beforeOpenJournal   func(string)
	afterOpenJournal    func(string)
}

// NewRepository 创建纯内存 Repository 配置，不打开或创建外部资源。
func NewRepository(rootPath string) (*Repository, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("session data root is required")
	}
	if !filepath.IsAbs(rootPath) {
		return nil, fmt.Errorf("session data root must be absolute")
	}
	return &Repository{rootPath: filepath.Clean(rootPath)}, nil
}

// OpenOrCreateRepository 构造 Repository 并显式打开或创建私有数据根。
func OpenOrCreateRepository(rootPath string) (*Repository, error) {
	repository, err := NewRepository(rootPath)
	if err != nil {
		return nil, err
	}
	if err := repository.OpenOrCreate(); err != nil {
		return nil, err
	}
	return repository, nil
}

// OpenOrCreate 获取 descriptor-bound 数据根；重复调用不会替换现有 owner。
func (repository *Repository) OpenOrCreate() error {
	if repository == nil {
		return fmt.Errorf("session repository is required")
	}
	if repository.root != nil {
		return nil
	}
	root, err := openOrCreateSecureRoot(repository.rootPath, repository.hooks)
	if err != nil {
		return err
	}
	repository.root = root
	return nil
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
	if repository == nil {
		return "", fmt.Errorf("session repository is required")
	}
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
	root, err := repository.openedRoot()
	if err != nil {
		return nil, err
	}
	relative, err := journalRelativePath(threadID)
	if err != nil {
		return nil, err
	}
	directoryName := path.Dir(relative)
	directory, err := root.openDirectory(directoryName, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	file, err := root.openJournal(directory, path.Base(relative), os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if err := validatePrivateFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	lease, err := acquireJournalLease(file, threadID)
	if err != nil {
		return nil, err
	}
	if err := directory.Sync(); err != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("sync session journal directory: %w", err)
	}
	return lease, nil
}

// Open 打开已有私有普通 journal，并在读取或修复前取得独占 lease。
func (repository *Repository) Open(ctx context.Context, threadID domain.ThreadID) (*JournalLease, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	root, err := repository.openedRoot()
	if err != nil {
		return nil, err
	}
	relative, err := journalRelativePath(threadID)
	if err != nil {
		return nil, err
	}
	directory, err := root.openDirectory(path.Dir(relative), false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	file, err := root.openJournal(directory, path.Base(relative), os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	if err := validatePrivateFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return acquireJournalLease(file, threadID)
}

// EnumerateJournals 按固定日期层级安全枚举 canonical root journal 候选。
func (repository *Repository) EnumerateJournals(ctx context.Context) ([]JournalLocation, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	root, err := repository.openedRoot()
	if err != nil {
		return nil, err
	}
	locations, err := root.enumerateJournals(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(locations, func(left int, right int) bool {
		return locations[left].RelativePath < locations[right].RelativePath
	})
	return locations, nil
}

func (repository *Repository) openedRoot() (*secureRoot, error) {
	if repository == nil || repository.root == nil {
		return nil, fmt.Errorf("session repository is not open")
	}
	return repository.root, nil
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

// Close 释放数据根句柄；尚未 OpenOrCreate 的纯配置可直接关闭。
func (repository *Repository) Close() error {
	if repository == nil || repository.root == nil {
		return nil
	}
	root := repository.root
	repository.root = nil
	return root.Close()
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
