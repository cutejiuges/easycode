package session

import (
	"bufio"
	"bytes"
	"context"
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

// LoadResult 是完成结构校验和可选尾部修复后的 journal 快照。
type LoadResult struct {
	Identity     Identity
	Records      []Record
	NextSequence uint64
	Repair       RepairReport
}

// Loader 负责有界结构加载，不解释 turn 或 Provider 语义。
type Loader struct {
	repository *Repository
}

// NewLoader 创建绑定受限 Repository 的 Loader。
func NewLoader(repository *Repository) (*Loader, error) {
	if repository == nil {
		return nil, fmt.Errorf("session repository is required")
	}
	return &Loader{repository: repository}, nil
}

// Load 完整校验 journal，并仅修复文件末尾的半行或未闭合 batch。
func (loader *Loader) Load(ctx context.Context, threadID domain.ThreadID) (LoadResult, error) {
	if err := contextError(ctx); err != nil {
		return LoadResult{}, err
	}
	file, err := loader.repository.Open(ctx, threadID)
	if err != nil {
		return LoadResult{}, err
	}
	result, loadErr := loadJournal(ctx, file, threadID)
	closeErr := file.Close()
	if loadErr != nil {
		return LoadResult{}, loadErr
	}
	if closeErr != nil {
		return LoadResult{}, fmt.Errorf("close session journal after load: %w", closeErr)
	}
	return result, nil
}

func loadJournal(ctx context.Context, file *os.File, expectedThreadID domain.ThreadID) (LoadResult, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return LoadResult{}, fmt.Errorf("seek session journal: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return LoadResult{}, fmt.Errorf("inspect session journal: %w", err)
	}
	originalSize := info.Size()
	reader := bufio.NewReaderSize(file, 64<<10)
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
			report, repairErr := repairTail(file, originalSize, truncateAt, RepairHalfLine)
			if repairErr != nil {
				return LoadResult{}, repairErr
			}
			return finalLoadResult(identity, committed, expectedThreadID, report), nil
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
		report, repairErr := repairTail(file, originalSize, batchOffset, RepairIncompleteBatch)
		if repairErr != nil {
			return LoadResult{}, repairErr
		}
		return finalLoadResult(identity, committed, expectedThreadID, report), nil
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

func repairTail(file *os.File, originalSize int64, truncateAt int64, kind RepairKind) (RepairReport, error) {
	if truncateAt < 0 || truncateAt > originalSize {
		return RepairReport{}, corrupt(fmt.Errorf("session repair offset is invalid"))
	}
	if err := file.Truncate(truncateAt); err != nil {
		return RepairReport{}, fmt.Errorf("truncate session journal tail: %w", err)
	}
	if err := file.Sync(); err != nil {
		return RepairReport{}, fmt.Errorf("sync repaired session journal: %w", err)
	}
	return RepairReport{
		Repaired: true, Kind: kind, TruncatedFrom: truncateAt, RemovedBytes: originalSize - truncateAt,
	}, nil
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
