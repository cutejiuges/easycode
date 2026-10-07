//go:build darwin || linux

package projectinstructions

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func loadHierarchy(
	startupDirectory string,
	maxBytes int,
	hooks loaderHooks,
) (map[int]discoveredDocument, int, bool, error) {
	fd, err := unix.Open(startupDirectory, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, 0, false, fmt.Errorf("open startup directory: %w", err)
	}
	directories := []*os.File{os.NewFile(uintptr(fd), "startup-directory")}
	for {
		current := directories[len(directories)-1]
		boundary, boundaryErr := hasProjectBoundary(current, hooks)
		if boundaryErr != nil {
			closeDirectories(directories)
			return nil, 0, false, boundaryErr
		}
		if boundary {
			rootDistance := len(directories) - 1
			documents, readErr := readInstructionHierarchy(directories, rootDistance, maxBytes, hooks)
			closeDirectories(directories)
			return documents, rootDistance, true, readErr
		}
		parentFD, parentErr := unix.Openat(
			int(current.Fd()), "..", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
		)
		if parentErr != nil {
			closeDirectories(directories)
			return nil, 0, false, fmt.Errorf("open project instruction parent directory: %w", parentErr)
		}
		parent := os.NewFile(uintptr(parentFD), "project-instruction-parent")
		same, compareErr := sameFileDescriptor(current, parent)
		if compareErr != nil {
			_ = parent.Close()
			closeDirectories(directories)
			return nil, 0, false, compareErr
		}
		if same {
			_ = parent.Close()
			documents, readErr := readInstructionHierarchy(directories[:1], 0, maxBytes, hooks)
			closeDirectories(directories)
			return documents, 0, false, readErr
		}
		directories = append(directories, parent)
	}
}

func readInstructionHierarchy(
	directories []*os.File,
	rootDistance int,
	maxBytes int,
	hooks loaderHooks,
) (map[int]discoveredDocument, error) {
	documents := make(map[int]discoveredDocument)
	for distance := rootDistance; distance >= 0; distance-- {
		document, exists, err := readDirectoryInstruction(directories[distance], maxBytes, hooks)
		if err != nil {
			return nil, err
		}
		if exists {
			documents[distance] = document
		}
	}
	return documents, nil
}

func closeDirectories(directories []*os.File) {
	for _, directory := range directories {
		_ = directory.Close()
	}
}

func readDirectoryInstruction(
	directory *os.File,
	maxBytes int,
	hooks loaderHooks,
) (discoveredDocument, bool, error) {
	for _, name := range [...]string{"AGENTS.md", "CLAUDE.md"} {
		file, exists, err := openRegularAt(directory, name, hooks)
		if err != nil {
			return discoveredDocument{}, false, err
		}
		if !exists {
			continue
		}
		if hooks.afterOpenFile != nil {
			hooks.afterOpenFile(name, file)
		}
		content, readErr := readInstructionFile(file, maxBytes)
		closeErr := file.Close()
		if readErr != nil {
			return discoveredDocument{}, false, readErr
		}
		if closeErr != nil {
			return discoveredDocument{}, false, closeErr
		}
		return discoveredDocument{name: name, content: content}, true, nil
	}
	return discoveredDocument{}, false, nil
}

func hasProjectBoundary(directory *os.File, hooks loaderHooks) (bool, error) {
	var before unix.Stat_t
	err := unix.Fstatat(int(directory.Fd()), ".git", &before, unix.AT_SYMLINK_NOFOLLOW)
	if missing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !regularOrDirectory(uint32(before.Mode)) {
		return false, errUnsafePath
	}
	if hooks.beforeOpenName != nil {
		hooks.beforeOpenName(".git")
	}
	fd, err := unix.Openat(int(directory.Fd()), ".git", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return false, errUnsafePath
		}
		return false, err
	}
	marker := os.NewFile(uintptr(fd), ".git")
	defer func() { _ = marker.Close() }()
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return false, err
	}
	if !regularOrDirectory(uint32(after.Mode)) || !sameStat(before, after) {
		return false, errUnsafePath
	}
	return true, nil
}

func openRegularAt(
	directory *os.File,
	name string,
	hooks loaderHooks,
) (*os.File, bool, error) {
	var before unix.Stat_t
	err := unix.Fstatat(int(directory.Fd()), name, &before, unix.AT_SYMLINK_NOFOLLOW)
	if missing(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, false, errUnsafePath
	}
	if hooks.beforeOpenName != nil {
		hooks.beforeOpenName(name)
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EISDIR) {
			return nil, false, errUnsafePath
		}
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), name)
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		_ = file.Close()
		return nil, false, err
	}
	if after.Mode&unix.S_IFMT != unix.S_IFREG || !sameStat(before, after) {
		_ = file.Close()
		return nil, false, errUnsafePath
	}
	return file, true, nil
}

func sameFileDescriptor(first *os.File, second *os.File) (bool, error) {
	var firstStat unix.Stat_t
	if err := unix.Fstat(int(first.Fd()), &firstStat); err != nil {
		return false, err
	}
	var secondStat unix.Stat_t
	if err := unix.Fstat(int(second.Fd()), &secondStat); err != nil {
		return false, err
	}
	return sameStat(firstStat, secondStat), nil
}

func sameStat(first unix.Stat_t, second unix.Stat_t) bool {
	return first.Dev == second.Dev && first.Ino == second.Ino
}

func regularOrDirectory(mode uint32) bool {
	kind := mode & unix.S_IFMT
	return kind == unix.S_IFREG || kind == unix.S_IFDIR
}
