package builtin

import (
	"context"
	"errors"
	"io"
	"sort"
)

const workspaceNameBatch = 256

func siftEntryNamesDown(names []string, root int) {
	for root*2+1 < len(names) {
		child := root*2 + 1
		if child+1 < len(names) && names[child+1] > names[child] {
			child++
		}
		if names[root] >= names[child] {
			return
		}
		names[root], names[child] = names[child], names[root]
		root = child
	}
}

// collectWalkerNames 保留既有工具的最小字节序前缀和全局额度，不套用文件候选目录政策。
// 超大目录继续分批枚举以保持原排序语义，但堆最多持有剩余额度再加一项。
func collectWalkerNames(ctx context.Context, directory workspaceDirectory, limit int) ([]string, error) {
	names := make([]string, 0, min(workspaceNameBatch, limit))
	ready := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch, err := directory.readEntryNames(workspaceNameBatch)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for _, name := range batch {
			if len(names) < limit {
				names = append(names, name)
				continue
			}
			if !ready {
				for root := len(names)/2 - 1; root >= 0; root-- {
					siftEntryNamesDown(names, root)
				}
				ready = true
			}
			if name < names[0] {
				names[0] = name
				siftEntryNamesDown(names, 0)
			}
		}
		if errors.Is(err, io.EOF) {
			sort.Strings(names)
			return []string(names), nil
		}
	}
}
