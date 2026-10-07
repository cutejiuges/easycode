// Package projectinstructions 负责从启动工作目录安全发现项目级指令。
package projectinstructions

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/fault"
)

const (
	DefaultMaxBytes            = 32 << 10
	instructionReadBufferBytes = 32 << 10
)

type loaderHooks struct {
	beforeOpenName func(string)
	afterOpenFile  func(string, *os.File)
}

// Loader 持有项目指令发现的有界策略和测试故障注入点。
type Loader struct {
	maxBytes int
	hooks    loaderHooks
}

type discoveredDocument struct {
	name    string
	content string
}

// NewLoader 创建不执行文件系统操作的项目指令 Loader。
func NewLoader(maxBytes int) (*Loader, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("project instruction byte limit must be positive")
	}
	if maxBytes > domain.MaxProjectInstructionsBytes {
		return nil, fmt.Errorf("project instruction byte limit exceeds %d bytes", domain.MaxProjectInstructionsBytes)
	}
	return &Loader{maxBytes: maxBytes}, nil
}

// Load 从规范化绝对启动目录发现一次不可变项目指令快照。
func (loader *Loader) Load(startupDirectory string) (domain.ProjectInstructionsSnapshot, error) {
	if loader == nil || loader.maxBytes <= 0 || loader.maxBytes > domain.MaxProjectInstructionsBytes {
		return domain.ProjectInstructionsSnapshot{}, fault.New(
			fault.CodeProjectInstructionsRead, "project instruction loader is invalid",
		)
	}
	clean := filepath.Clean(startupDirectory)
	if !filepath.IsAbs(clean) || clean != startupDirectory {
		return domain.ProjectInstructionsSnapshot{}, fault.New(
			fault.CodeProjectInstructionsRead, "startup directory must be a normalized absolute path",
		)
	}
	documents, rootDistance, foundRoot, err := loadHierarchy(clean, loader.maxBytes, loader.hooks)
	if err != nil {
		return domain.ProjectInstructionsSnapshot{}, classifyLoadError(err)
	}
	if !foundRoot {
		rootDistance = 0
	}
	ordered := make([]domain.ProjectInstructionDocument, 0, rootDistance+1)
	rootPath := clean
	for index := 0; index < rootDistance; index++ {
		rootPath = filepath.Dir(rootPath)
	}
	for distance := rootDistance; distance >= 0; distance-- {
		discovered, exists := documents[distance]
		if !exists {
			continue
		}
		directory := clean
		for index := 0; index < distance; index++ {
			directory = filepath.Dir(directory)
		}
		relativeDirectory, relativeErr := filepath.Rel(rootPath, directory)
		if relativeErr != nil {
			return domain.ProjectInstructionsSnapshot{}, fault.New(
				fault.CodeProjectInstructionsRead, "project instruction source is unavailable",
			)
		}
		source := discovered.name
		if relativeDirectory != "." {
			source = filepath.ToSlash(filepath.Join(relativeDirectory, discovered.name))
		}
		document, documentErr := domain.NewProjectInstructionDocument(source, discovered.content)
		if documentErr != nil {
			return domain.ProjectInstructionsSnapshot{}, fault.New(
				fault.CodeProjectInstructionsRead, "project instruction snapshot is invalid",
			)
		}
		ordered = append(ordered, document)
	}
	snapshot, err := domain.NewProjectInstructionsSnapshot(ordered, loader.maxBytes)
	if err != nil {
		return domain.ProjectInstructionsSnapshot{}, fault.New(
			fault.CodeProjectInstructionsRead, "project instruction snapshot is invalid",
		)
	}
	return snapshot, nil
}

func readInstructionFile(file *os.File, maxBytes int) (string, error) {
	if file == nil || maxBytes <= 0 || maxBytes > domain.MaxProjectInstructionsBytes {
		return "", fmt.Errorf("instruction file is unavailable")
	}
	reader := bufio.NewReaderSize(file, instructionReadBufferBytes)
	var retained strings.Builder
	retained.Grow(min(maxBytes, instructionReadBufferBytes))
	for {
		runeValue, size, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if runeValue == utf8.RuneError && size == 1 {
			return "", errInvalidUTF8
		}
		if retained.Len()+size <= maxBytes {
			retained.WriteRune(runeValue)
		}
	}
	return retained.String(), nil
}

func classifyLoadError(err error) error {
	switch {
	case errors.Is(err, errUnsafePath):
		return fault.New(fault.CodeProjectInstructionsUnsafe, "project instruction path is unsafe")
	case errors.Is(err, errInvalidUTF8):
		return fault.New(fault.CodeProjectInstructionsInvalidUTF8, "project instruction content is not valid UTF-8")
	default:
		return fault.New(fault.CodeProjectInstructionsRead, "project instructions could not be read")
	}
}

var (
	errUnsafePath  = errors.New("unsafe project instruction path")
	errInvalidUTF8 = errors.New("invalid project instruction UTF-8")
)

func missing(err error) bool { return errors.Is(err, fs.ErrNotExist) }
