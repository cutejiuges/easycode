package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type journalFile interface {
	io.Writer
	Sync() error
	Close() error
}

type writerRequest struct {
	drafts   []RecordDraft
	response chan appendResponse
}

type appendResponse struct {
	records []Record
	err     error
}

const journalRequestCapacity = 64

// JournalWriter 由单 goroutine 串行拥有文件和下一序号。
type JournalWriter struct {
	file     journalFile
	identity Identity
	clock    func() time.Time
	requests chan writerRequest
	closeReq chan struct{}
	done     chan struct{}

	mu       sync.Mutex
	closing  bool
	result   JournalCloseResult
	poisoned atomic.Bool
}

// JournalCloseResult 分别记录先前的 writer poison 和最终同步、关闭结果。
type JournalCloseResult struct {
	Poisoned bool
	SyncErr  error
	CloseErr error
}

// CleanupError 只报告最终同步或描述符关闭的失败。
func (result JournalCloseResult) CleanupError() error {
	if err := errors.Join(result.SyncErr, result.CloseErr); err != nil {
		return fmt.Errorf("close session writer: %w", err)
	}
	return nil
}

// Err 保留 Close 的聚合错误契约。
func (result JournalCloseResult) Err() error {
	if result.Poisoned {
		return errors.Join(fmt.Errorf("session writer is poisoned"), result.SyncErr, result.CloseErr)
	}
	return result.CleanupError()
}

// StartJournalWriter 接管 lease，并从 nextSequence 继续追加。
func StartJournalWriter(lease *JournalLease, identity Identity, nextSequence uint64) (*JournalWriter, error) {
	if err := validateJournalWriter(identity, nextSequence, time.Now, journalRequestCapacity); err != nil {
		return nil, err
	}
	file, err := lease.transfer()
	if err != nil {
		return nil, err
	}
	return startOwnedJournalWriter(file, identity, nextSequence, time.Now, journalRequestCapacity), nil
}

func validateJournalWriter(
	identity Identity,
	nextSequence uint64,
	clock func() time.Time,
	requestCapacity int,
) error {
	if !identity.SessionID.Valid() || !identity.ThreadID.Valid() {
		return fmt.Errorf("session writer identity is invalid")
	}
	if nextSequence == 0 {
		return fmt.Errorf("session writer next sequence is invalid")
	}
	if clock == nil {
		return fmt.Errorf("session writer clock is required")
	}
	if requestCapacity <= 0 {
		return fmt.Errorf("session writer request capacity is invalid")
	}
	return nil
}

func startOwnedJournalWriter(
	file journalFile,
	identity Identity,
	nextSequence uint64,
	clock func() time.Time,
	requestCapacity int,
) *JournalWriter {
	writer := &JournalWriter{
		file: file, identity: identity, clock: clock,
		requests: make(chan writerRequest, requestCapacity), closeReq: make(chan struct{}),
		done: make(chan struct{}),
	}
	go writer.run(nextSequence)
	return writer
}

// AppendBatch 编码并 durable 追加一个全有或全无恢复语义的 batch。
func (writer *JournalWriter) AppendBatch(ctx context.Context, drafts []RecordDraft) ([]Record, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if len(drafts) == 0 || len(drafts) > math.MaxUint32 {
		return nil, fmt.Errorf("session batch size is invalid")
	}
	prepared := make([]RecordDraft, 0, len(drafts))
	for _, draft := range drafts {
		if draft.EventKind() == EventSessionTitleUpdated && len(drafts) != 1 {
			return nil, fmt.Errorf("session title record requires a single-record batch")
		}
		if err := draft.validate(); err != nil {
			return nil, err
		}
		prepared = append(prepared, draft.clone())
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	request := writerRequest{drafts: prepared, response: make(chan appendResponse, 1)}
	writer.mu.Lock()
	if writer.closing {
		writer.mu.Unlock()
		return nil, fmt.Errorf("session writer is closed")
	}
	if writer.poisoned.Load() {
		writer.mu.Unlock()
		return nil, fmt.Errorf("session writer is poisoned")
	}
	if err := ctx.Err(); err != nil {
		writer.mu.Unlock()
		return nil, fmt.Errorf("session append cancelled: %w", err)
	}
	select {
	case writer.requests <- request:
		writer.mu.Unlock()
	default:
		writer.mu.Unlock()
		return nil, fmt.Errorf("session writer queue is full")
	}
	response := <-request.response
	return response.records, response.err
}

// Poisoned 判断 writer 是否遇到无法确认 durable 状态的写入错误。
func (writer *JournalWriter) Poisoned() bool {
	return writer != nil && writer.poisoned.Load()
}

// Close 停止接收新 batch，等待已接收请求，最终 Sync 并关闭文件。
func (writer *JournalWriter) Close(ctx context.Context) error {
	result, err := writer.CloseResult(ctx)
	if err != nil {
		return err
	}
	return result.Err()
}

// CloseResult 在最终关闭完成后返回实际结果。等待取消时，writer 继续持有关闭责任。
func (writer *JournalWriter) CloseResult(ctx context.Context) (JournalCloseResult, error) {
	if writer == nil {
		return JournalCloseResult{}, nil
	}
	if ctx == nil {
		return JournalCloseResult{}, fmt.Errorf("session context is required")
	}
	writer.mu.Lock()
	if !writer.closing {
		writer.closing = true
		close(writer.closeReq)
	}
	done := writer.done
	writer.mu.Unlock()
	select {
	case <-done:
		return writer.result, nil
	default:
	}
	select {
	case <-done:
		return writer.result, nil
	case <-ctx.Done():
		return JournalCloseResult{}, fmt.Errorf("session writer close cancelled: %w", ctx.Err())
	}
}

func (writer *JournalWriter) run(nextSequence uint64) {
	defer close(writer.done)
	for {
		select {
		case request := <-writer.requests:
			nextSequence = writer.handleAppend(nextSequence, request)
		case <-writer.closeReq:
			for {
				select {
				case request := <-writer.requests:
					nextSequence = writer.handleAppend(nextSequence, request)
				default:
					writer.result = writer.finish()
					return
				}
			}
		}
	}
}

func (writer *JournalWriter) handleAppend(nextSequence uint64, request writerRequest) uint64 {
	if writer.poisoned.Load() {
		request.response <- appendResponse{err: fmt.Errorf("session writer is poisoned")}
		return nextSequence
	}
	records, err := writer.append(nextSequence, request.drafts)
	if err != nil {
		writer.poisoned.Store(true)
		request.response <- appendResponse{err: err}
		return nextSequence
	}
	request.response <- appendResponse{records: records}
	return nextSequence + uint64(len(records))
}

func (writer *JournalWriter) append(nextSequence uint64, drafts []RecordDraft) ([]Record, error) {
	batchSize := uint32(len(drafts))
	batchID := nextSequence
	var buffer bytes.Buffer
	records := make([]Record, 0, len(drafts))
	for index, draft := range drafts {
		record := Record{
			Sequence:  nextSequence + uint64(index),
			Timestamp: writer.clock().UTC(), SessionID: writer.identity.SessionID,
			ThreadID: writer.identity.ThreadID, ParentThreadID: draft.parentThreadID,
			TurnID: draft.turnID, EventKind: draft.descriptor.Kind,
			BatchID: batchID, BatchIndex: uint32(index), BatchSize: batchSize,
			Payload: draft.PayloadBytes(),
		}
		sealed, encoded, err := EncodeRecord(record)
		if err != nil {
			return nil, err
		}
		if buffer.Len()+len(encoded)+1 > MaxBatchBytes {
			return nil, fmt.Errorf("session batch exceeds size limit")
		}
		buffer.Write(encoded)
		buffer.WriteByte('\n')
		records = append(records, sealed)
	}
	written, err := writer.file.Write(buffer.Bytes())
	if err != nil {
		return nil, &journalMutationUncertain{cause: fmt.Errorf("append session batch: %w", err)}
	}
	if written != buffer.Len() {
		return nil, &journalMutationUncertain{cause: fmt.Errorf("append session batch: %w", io.ErrShortWrite)}
	}
	if err := writer.file.Sync(); err != nil {
		return nil, &journalMutationUncertain{cause: fmt.Errorf("sync session batch: %w", err)}
	}
	return cloneRecords(records), nil
}

func (writer *JournalWriter) finish() JournalCloseResult {
	return JournalCloseResult{
		Poisoned: writer.poisoned.Load(),
		SyncErr:  writer.file.Sync(),
		CloseErr: writer.file.Close(),
	}
}

func cloneRecords(records []Record) []Record {
	cloned := make([]Record, len(records))
	for index, record := range records {
		cloned[index] = cloneRecord(record)
	}
	return cloned
}
