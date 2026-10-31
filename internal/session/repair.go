package session

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// repairTicket 绑定只读扫描和原 lease，不从可变 LoadResult 字段重建权限。
type repairTicket struct {
	lease        *JournalLease
	size, offset int64
	digest       [sha256.Size]byte
	kind         RepairKind
}

// NeedsRepair 表示只读扫描发现未完成尾部，不代表文件已被修复。
func (result LoadResult) NeedsRepair() bool { return result.repair != nil }

// Repair 在调用方完成语义和 Provider 验证后，显式修复同一 lease 的尾部。
func (loader *Loader) Repair(ctx context.Context, lease *JournalLease, loaded LoadResult) (RepairReport, error) {
	return loader.repairWith(ctx, lease, loaded, fileRepairOperations{})
}

type repairOperations interface {
	Truncate(*os.File, int64) error
	Sync(*os.File) error
}
type fileRepairOperations struct{}

func (fileRepairOperations) Truncate(file *os.File, size int64) error { return file.Truncate(size) }
func (fileRepairOperations) Sync(file *os.File) error                 { return file.Sync() }

func (loader *Loader) repairWith(ctx context.Context, lease *JournalLease, loaded LoadResult, operations repairOperations) (RepairReport, error) {
	if err := contextError(ctx); err != nil {
		return RepairReport{}, err
	}
	ticket := loaded.repair
	if loader == nil || ticket == nil || ticket.lease != lease || operations == nil {
		return RepairReport{}, fmt.Errorf("session repair authority is invalid")
	}
	file, _, err := lease.beginIO()
	if err != nil {
		return RepairReport{}, err
	}
	defer lease.endIO()
	lease.mu.Lock()
	current := lease.repair == ticket
	lease.mu.Unlock()
	if !current {
		return RepairReport{}, fmt.Errorf("session repair authority is stale")
	}
	info, err := file.Stat()
	if err != nil {
		return RepairReport{}, fmt.Errorf("inspect session repair: %w", err)
	}
	if info.Size() != ticket.size || ticket.offset < 0 || ticket.offset >= ticket.size {
		return RepairReport{}, fmt.Errorf("session repair source changed")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, io.NewSectionReader(file, 0, ticket.size)); err != nil {
		return RepairReport{}, fmt.Errorf("verify session repair: %w", err)
	}
	var actual [sha256.Size]byte
	copy(actual[:], digest.Sum(nil))
	if actual != ticket.digest {
		return RepairReport{}, fmt.Errorf("session repair source changed")
	}
	if err := contextError(ctx); err != nil {
		return RepairReport{}, err
	}
	// 此处接收截断；从现在开始必须等待确定结果，取消不再改变返回值。
	err = operations.Truncate(file, ticket.offset)
	if err == nil {
		err = operations.Sync(file)
	}
	lease.mu.Lock()
	lease.repair = nil
	if err != nil {
		lease.poisoned = true
	}
	lease.mu.Unlock()
	if err != nil {
		return RepairReport{}, fmt.Errorf("repair session journal failed: %w", err)
	}
	return RepairReport{Repaired: true, Kind: ticket.kind, TruncatedFrom: ticket.offset, RemovedBytes: ticket.size - ticket.offset}, nil
}
