package builtin

import (
	"context"
	"errors"
	"sort"
	"strings"

	"easycode/internal/tool"
)

const (
	maxWorkspaceDepth   = 64
	maxWorkspaceEntries = 200_000
)

type workspaceFile struct {
	relativePath string
	size         int64
}

type walkResult struct {
	files            []workspaceFile
	visitedEntries   int
	incompleteReason tool.SearchIncompleteReason
	skipped          mutableSkipCounts
}

type mutableSkipCounts struct {
	binary      int
	invalidUTF8 int
	tooLarge    int
	unreadable  int
	unsupported int
	disappeared int
}

func (counts mutableSkipCounts) freeze() tool.SearchSkipCounts {
	frozen, _ := tool.NewSearchSkipCounts(counts.binary, counts.invalidUTF8, counts.tooLarge, counts.unreadable, counts.unsupported, counts.disappeared)
	return frozen
}

func isVCSMetadataDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".jj", ".sl", ".bzr":
		return true
	default:
		return false
	}
}

func walkWorkspace(ctx context.Context, workspace *Workspace, start string) (walkResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return walkResult{}, context.Canceled
	}
	if workspace == nil || workspace.handle == nil {
		return walkResult{}, errors.New("workspace is closed")
	}
	directory, err := workspace.handle.openDirectory(start, workspace.hooks)
	if err != nil {
		return walkResult{}, errors.New("search root is unavailable")
	}
	result := walkResult{files: make([]workspaceFile, 0)}
	err = walkDirectory(ctx, directory, start, 0, workspace.hooks, &result)
	closeErr := directory.close()
	if err != nil {
		return walkResult{}, err
	}
	if closeErr != nil {
		return walkResult{}, errors.New("search directory could not be closed")
	}
	sort.Slice(result.files, func(left int, right int) bool {
		return result.files[left].relativePath < result.files[right].relativePath
	})
	return result, nil
}

func walkDirectory(ctx context.Context, directory workspaceDirectory, relative string, depth int, hooks workspaceHooks, result *walkResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxWorkspaceDepth {
		result.incompleteReason = tool.SearchEntryLimitReached
		return nil
	}
	names, err := directory.readEntryNames()
	if err != nil {
		result.skipped.unreadable++
		return nil
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if result.visitedEntries >= maxWorkspaceEntries {
			result.incompleteReason = tool.SearchEntryLimitReached
			return nil
		}
		result.visitedEntries++
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			result.skipped.unsupported++
			continue
		}
		child, openErr := directory.openChild(name, hooks)
		if openErr != nil {
			classifyWalkOpenError(openErr, &result.skipped)
			continue
		}
		childPath := name
		if relative != "" {
			childPath = relative + "/" + name
		}
		switch child.kind() {
		case workspaceNodeDirectory:
			if isVCSMetadataDirectory(name) {
				if closeErr := child.close(); closeErr != nil {
					return errors.New("search directory could not be closed")
				}
				continue
			}
			if depth == maxWorkspaceDepth {
				result.incompleteReason = tool.SearchEntryLimitReached
			} else if err := walkDirectory(ctx, child.directory(), childPath, depth+1, hooks, result); err != nil {
				_ = child.close()
				return err
			}
		case workspaceNodeRegular:
			result.files = append(result.files, workspaceFile{relativePath: childPath, size: child.size()})
		default:
			result.skipped.unsupported++
		}
		if closeErr := child.close(); closeErr != nil {
			return errors.New("search child could not be closed")
		}
		if result.incompleteReason != tool.SearchComplete {
			return nil
		}
	}
	return nil
}

func classifyWalkOpenError(err error, counts *mutableSkipCounts) {
	switch {
	case errors.Is(err, errTargetNotReadable):
		counts.unreadable++
	case errors.Is(err, errTargetNotRegular):
		counts.unsupported++
	default:
		counts.disappeared++
	}
}
