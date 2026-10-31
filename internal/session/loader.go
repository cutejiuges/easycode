package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"easycode/internal/domain"
)

// RepairKind 区分允许自动截断的两种尾部损坏。
type RepairKind string

const (
	RepairNone            RepairKind = ""
	RepairHalfLine        RepairKind = "truncated_half_line"
	RepairIncompleteBatch RepairKind = "truncated_incomplete_batch"
)

// RepairReport 只报告结构信息，不回显被截断的 payload。
type RepairReport struct {
	Repaired      bool       `json:"repaired"`
	Kind          RepairKind `json:"kind,omitempty"`
	TruncatedFrom int64      `json:"truncated_from,omitempty"`
	RemovedBytes  int64      `json:"removed_bytes,omitempty"`
}

// LoadResult 是完成只读结构校验后的 journal 快照。
type LoadResult struct {
	Identity     Identity
	Records      []Record
	NextSequence uint64
	Repair       RepairReport
	repair       *repairTicket
}

// Loader 负责有界结构加载，不解释 turn 或 Provider 语义。
type Loader struct{}

// NewLoader 创建无外部副作用的 Loader。
func NewLoader() *Loader {
	return &Loader{}
}

// Load 借用 lease 的同一 handle 完整校验 journal，并返回待修复尾部；调用方完成语义验证后显式调用 Repair。
func (loader *Loader) Load(ctx context.Context, lease *JournalLease) (LoadResult, error) {
	if err := contextError(ctx); err != nil {
		return LoadResult{}, err
	}
	if loader == nil {
		return LoadResult{}, fmt.Errorf("session loader is required")
	}
	file, threadID, err := lease.beginIO()
	if err != nil {
		return LoadResult{}, err
	}
	defer lease.endIO()
	result, err := loadJournal(ctx, file, threadID)
	if err != nil {
		return LoadResult{}, err
	}
	if result.repair != nil {
		result.repair.lease = lease
	}
	lease.mu.Lock()
	lease.repair = result.repair
	lease.mu.Unlock()
	return result, nil
}

func loadJournal(ctx context.Context, file *os.File, expectedThreadID domain.ThreadID) (result LoadResult, resultErr error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return LoadResult{}, fmt.Errorf("seek session journal: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return LoadResult{}, fmt.Errorf("inspect session journal: %w", err)
	}
	originalSize := info.Size()
	digest := sha256.New()
	reader := bufio.NewReaderSize(io.TeeReader(file, digest), 64<<10)
	defer func() {
		if resultErr == nil && result.repair != nil {
			copy(result.repair.digest[:], digest.Sum(nil))
		}
	}()
	committed := make([]Record, 0)
	currentBatch := make([]Record, 0)
	var identity Identity
	expectedSequence := uint64(1)
	offset := int64(0)
	batchOffset := int64(0)
	batchBytes := 0

	for {
		if err := contextError(ctx); err != nil {
			return LoadResult{}, err
		}
		lineOffset := offset
		line, terminated, readErr := readBoundedLine(reader)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return LoadResult{}, corrupt(readErr)
		}
		if errors.Is(readErr, io.EOF) && len(line) == 0 {
			break
		}
		consumed := len(line)
		if terminated {
			consumed++
		}
		offset += int64(consumed)

		if !terminated {
			truncateAt := lineOffset
			if len(currentBatch) > 0 {
				truncateAt = batchOffset
			}
			result := finalLoadResult(identity, committed, expectedThreadID, RepairReport{})
			result.repair = &repairTicket{size: originalSize, offset: truncateAt, kind: RepairHalfLine}
			return result, nil
		}

		record, decodeErr := DecodeRecord(line)
		if decodeErr != nil {
			return LoadResult{}, corrupt(decodeErr)
		}
		if record.Sequence != expectedSequence {
			return LoadResult{}, corrupt(fmt.Errorf("session sequence is not continuous"))
		}
		if record.ThreadID != expectedThreadID {
			return LoadResult{}, corrupt(fmt.Errorf("session thread identity changed"))
		}
		if identity.SessionID == "" {
			identity = Identity{SessionID: record.SessionID, ThreadID: record.ThreadID}
		} else if record.SessionID != identity.SessionID || record.ThreadID != identity.ThreadID {
			return LoadResult{}, corrupt(fmt.Errorf("session identity changed"))
		}

		if len(currentBatch) == 0 {
			if record.BatchIndex != 0 || record.BatchID != record.Sequence {
				return LoadResult{}, corrupt(fmt.Errorf("session batch does not start at its first sequence"))
			}
			batchOffset = lineOffset
			batchBytes = 0
		} else {
			first := currentBatch[0]
			if record.BatchID != first.BatchID || record.BatchSize != first.BatchSize ||
				record.BatchIndex != uint32(len(currentBatch)) {
				return LoadResult{}, corrupt(fmt.Errorf("session batch boundary is invalid"))
			}
		}
		batchBytes += consumed
		if batchBytes > MaxBatchBytes {
			return LoadResult{}, corrupt(fmt.Errorf("session batch exceeds size limit"))
		}
		currentBatch = append(currentBatch, record)
		expectedSequence++
		if uint32(len(currentBatch)) == record.BatchSize {
			committed = append(committed, currentBatch...)
			currentBatch = currentBatch[:0]
			batchBytes = 0
		}
	}

	if len(currentBatch) > 0 {
		result := finalLoadResult(identity, committed, expectedThreadID, RepairReport{})
		result.repair = &repairTicket{size: originalSize, offset: batchOffset, kind: RepairIncompleteBatch}
		return result, nil
	}
	return finalLoadResult(identity, committed, expectedThreadID, RepairReport{}), nil
}

func readBoundedLine(reader *bufio.Reader) ([]byte, bool, error) {
	var line bytes.Buffer
	for {
		fragment, err := reader.ReadSlice('\n')
		if line.Len()+len(fragment) > MaxRecordBytes+1 {
			return nil, false, fmt.Errorf("session record exceeds size limit")
		}
		line.Write(fragment)
		switch {
		case err == nil:
			value := line.Bytes()
			return append([]byte(nil), value[:len(value)-1]...), true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return append([]byte(nil), line.Bytes()...), false, io.EOF
		default:
			return nil, false, err
		}
	}
}

func finalLoadResult(
	identity Identity,
	records []Record,
	expectedThreadID domain.ThreadID,
	repair RepairReport,
) LoadResult {
	nextSequence := uint64(1)
	if len(records) > 0 {
		nextSequence = records[len(records)-1].Sequence + 1
	}
	if identity.ThreadID == "" {
		identity.ThreadID = expectedThreadID
	}
	return LoadResult{
		Identity: identity, Records: cloneRecords(records), NextSequence: nextSequence, Repair: repair,
	}
}

func corrupt(cause error) error {
	return fmt.Errorf("session journal is corrupted: %w", cause)
}
