//go:build darwin || linux

package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"easycode/internal/domain"
	"golang.org/x/sys/unix"
)

type secureRoot struct {
	file  *os.File
	hooks securePathHooks
}

func openOrCreateSecureRoot(rootPath string, hooks securePathHooks) (*secureRoot, error) {
	parentPath := filepath.Dir(rootPath)
	if err := os.MkdirAll(parentPath, 0o700); err != nil {
		return nil, fmt.Errorf("create session data root parent: %w", err)
	}
	parentFD, err := unix.Open(parentPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open session data root parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	defer func() { _ = parent.Close() }()

	base := filepath.Base(rootPath)
	callSecurePathHook(hooks.beforeOpenRoot, rootPath)
	rootFD, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	created := false
	if errors.Is(err, fs.ErrNotExist) {
		if mkdirErr := unix.Mkdirat(parentFD, base, 0o700); mkdirErr != nil {
			return nil, fmt.Errorf("create session data root: %w", mkdirErr)
		}
		created = true
		rootFD, err = unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("session data root must be a real directory: %w", err)
	}
	rootFile := os.NewFile(uintptr(rootFD), rootPath)
	info, statErr := rootFile.Stat()
	if statErr != nil {
		_ = rootFile.Close()
		return nil, fmt.Errorf("inspect session data root: %w", statErr)
	}
	if !info.IsDir() {
		_ = rootFile.Close()
		return nil, fmt.Errorf("session data root must be a real directory")
	}
	if permissionErr := validatePrivatePermissions(info, 0o700, "session data root"); permissionErr != nil {
		_ = rootFile.Close()
		return nil, permissionErr
	}
	if created {
		if syncErr := parent.Sync(); syncErr != nil {
			_ = rootFile.Close()
			return nil, fmt.Errorf("sync session data root parent: %w", syncErr)
		}
	}
	callSecurePathHook(hooks.afterOpenRoot, rootPath)
	return &secureRoot{file: rootFile, hooks: hooks}, nil
}

func (root *secureRoot) openDirectory(relative string, create bool) (*os.File, error) {
	if root == nil || root.file == nil {
		return nil, fmt.Errorf("session data root is closed")
	}
	currentFD, err := unix.Dup(int(root.file.Fd()))
	if err != nil {
		return nil, fmt.Errorf("duplicate session data root handle: %w", err)
	}
	unix.CloseOnExec(currentFD)
	current := os.NewFile(uintptr(currentFD), root.file.Name())
	currentRelative := ""
	for _, component := range splitPath(relative) {
		if currentRelative == "" {
			currentRelative = component
		} else {
			currentRelative = path.Join(currentRelative, component)
		}
		callSecurePathHook(root.hooks.beforeOpenComponent, currentRelative)
		nextFD, openErr := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		created := false
		if create && errors.Is(openErr, fs.ErrNotExist) {
			if mkdirErr := unix.Mkdirat(currentFD, component, 0o700); mkdirErr != nil {
				_ = current.Close()
				return nil, fmt.Errorf("create session directory: %w", mkdirErr)
			}
			created = true
			nextFD, openErr = unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		}
		if openErr != nil {
			_ = current.Close()
			return nil, fmt.Errorf("session directory must be a real directory: %w", openErr)
		}
		next := os.NewFile(uintptr(nextFD), currentRelative)
		info, statErr := next.Stat()
		if statErr != nil || !info.IsDir() {
			_ = next.Close()
			_ = current.Close()
			if statErr != nil {
				return nil, fmt.Errorf("inspect session directory: %w", statErr)
			}
			return nil, fmt.Errorf("session directory must be a real directory")
		}
		if permissionErr := validatePrivatePermissions(info, 0o700, "session directory"); permissionErr != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, permissionErr
		}
		if created {
			if syncErr := current.Sync(); syncErr != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, fmt.Errorf("sync session directory parent: %w", syncErr)
			}
		}
		_ = current.Close()
		current = next
		currentFD = nextFD
		callSecurePathHook(root.hooks.afterOpenComponent, currentRelative)
	}
	return current, nil
}

func (root *secureRoot) openJournal(
	directory *os.File,
	name string,
	flags int,
	permissions fs.FileMode,
) (*os.File, error) {
	if root == nil || directory == nil || name == "" || path.Base(name) != name {
		return nil, fmt.Errorf("session journal path is invalid")
	}
	relative := path.Join(directory.Name(), name)
	callSecurePathHook(root.hooks.beforeOpenJournal, relative)
	fd, err := unix.Openat(
		int(directory.Fd()), name,
		flags|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		uint32(permissions.Perm()),
	)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EISDIR) {
			return nil, fmt.Errorf("session journal must be a regular file: %w", err)
		}
		return nil, fmt.Errorf("open session journal: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	callSecurePathHook(root.hooks.afterOpenJournal, relative)
	return file, nil
}

func (root *secureRoot) enumerateJournals(ctx context.Context) ([]JournalLocation, error) {
	years, err := readDirectoryNames(root.file)
	if err != nil {
		return nil, fmt.Errorf("read session data root: %w", err)
	}
	locations := make([]JournalLocation, 0)
	for _, year := range years {
		yearMatches, yearValid := classifyDecimalComponent(year, 4, 1, 9999)
		if !yearMatches {
			continue
		}
		if !yearValid {
			return nil, fmt.Errorf("session year directory is invalid")
		}
		yearDirectory, openErr := root.openDirectory(year, false)
		if openErr != nil {
			return nil, openErr
		}
		months, readErr := readDirectoryNames(yearDirectory)
		_ = yearDirectory.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read session year directory: %w", readErr)
		}
		for _, month := range months {
			monthMatches, monthValid := classifyDecimalComponent(month, 2, 1, 12)
			if !monthMatches {
				continue
			}
			if !monthValid {
				return nil, fmt.Errorf("session month directory is invalid")
			}
			yearMonth := path.Join(year, month)
			monthDirectory, monthErr := root.openDirectory(yearMonth, false)
			if monthErr != nil {
				return nil, monthErr
			}
			days, dayReadErr := readDirectoryNames(monthDirectory)
			_ = monthDirectory.Close()
			if dayReadErr != nil {
				return nil, fmt.Errorf("read session month directory: %w", dayReadErr)
			}
			for _, day := range days {
				dayMatches, _ := classifyDecimalComponent(day, 2, 1, 31)
				if !dayMatches {
					continue
				}
				if !validDateComponents(year, month, day) {
					return nil, fmt.Errorf("session day directory is invalid")
				}
				yearMonthDay := path.Join(yearMonth, day)
				dayDirectory, dayErr := root.openDirectory(yearMonthDay, false)
				if dayErr != nil {
					return nil, dayErr
				}
				journals, journalReadErr := readDirectoryNames(dayDirectory)
				if journalReadErr != nil {
					_ = dayDirectory.Close()
					return nil, fmt.Errorf("read session day directory: %w", journalReadErr)
				}
				for _, journal := range journals {
					if err := contextError(ctx); err != nil {
						_ = dayDirectory.Close()
						return nil, err
					}
					threadID, candidate, candidateErr := parseJournalCandidate(journal, year, month, day)
					if candidateErr != nil {
						_ = dayDirectory.Close()
						return nil, candidateErr
					}
					if !candidate {
						continue
					}
					file, fileErr := root.openJournal(dayDirectory, journal, os.O_RDWR, 0)
					if fileErr != nil {
						_ = dayDirectory.Close()
						return nil, fileErr
					}
					validationErr := validatePrivateFile(file)
					closeErr := file.Close()
					if validationErr != nil {
						_ = dayDirectory.Close()
						return nil, validationErr
					}
					if closeErr != nil {
						_ = dayDirectory.Close()
						return nil, fmt.Errorf("close enumerated session journal: %w", closeErr)
					}
					locations = append(locations, JournalLocation{
						ThreadID: threadID, RelativePath: path.Join(yearMonthDay, journal),
					})
				}
				if closeErr := dayDirectory.Close(); closeErr != nil {
					return nil, fmt.Errorf("close session day directory: %w", closeErr)
				}
			}
		}
	}
	return locations, nil
}

func readDirectoryNames(directory *os.File) ([]string, error) {
	if directory == nil {
		return nil, fmt.Errorf("session directory is required")
	}
	if _, err := directory.Seek(0, 0); err != nil {
		return nil, err
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func classifyDecimalComponent(value string, width int, minimum int, maximum int) (bool, bool) {
	if len(value) != width {
		return false, false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false, false
		}
	}
	parsed, err := strconv.Atoi(value)
	return true, err == nil && parsed >= minimum && parsed <= maximum
}

func validDateComponents(year string, month string, day string) bool {
	_, validDay := classifyDecimalComponent(day, 2, 1, 31)
	if !validDay {
		return false
	}
	yearValue, _ := strconv.Atoi(year)
	monthValue, _ := strconv.Atoi(month)
	dayValue, _ := strconv.Atoi(day)
	value := time.Date(yearValue, time.Month(monthValue), dayValue, 0, 0, 0, 0, time.UTC)
	return value.Year() == yearValue && int(value.Month()) == monthValue && value.Day() == dayValue
}

func parseJournalCandidate(name string, year string, month string, day string) (domain.ThreadID, bool, error) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false, nil
	}
	threadID, err := domain.ParseThreadID(strings.TrimSuffix(name, ".jsonl"))
	if err != nil {
		return "", false, fmt.Errorf("session journal name is invalid")
	}
	timestamp, err := threadID.Time()
	if err != nil || timestamp.Format("2006") != year || timestamp.Format("01") != month || timestamp.Format("02") != day {
		return "", false, fmt.Errorf("session journal date does not match its identity")
	}
	return threadID, true, nil
}

func (root *secureRoot) Close() error {
	if root == nil || root.file == nil {
		return nil
	}
	file := root.file
	root.file = nil
	return file.Close()
}

func callSecurePathHook(hook func(string), value string) {
	if hook != nil {
		hook(value)
	}
}
