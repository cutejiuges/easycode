package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type journalFile interface {
	io.Writer
	Sync() error
	Close() error
}

type writerRequest struct {
	drafts   []preparedDraft
	response chan appendResponse
}

type appendResponse struct {
	records []Record
	err     error
}

type preparedDraft struct {
	descriptor     Descriptor
	parentThreadID string
	turnID         string
	payload        json.RawMessage
}

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
	closeErr error
	poisoned atomic.Bool
}

// NewJournalWriter 接管文件所有权，并从 nextSequence 继续追加。
func NewJournalWriter(file *os.File, identity Identity, nextSequence uint64) (*JournalWriter, error) {
	return newJournalWriter(file, identity, nextSequence, time.Now)
}

func newJournalWriter(
	file journalFile,
	identity Identity,
	nextSequence uint64,
	clock func() time.Time,
) (*JournalWriter, error) {
	if file == nil {
		return nil, fmt.Errorf("session journal file is required")
	}
	if !identity.SessionID.Valid() || !identity.ThreadID.Valid() {
		return nil, fmt.Errorf("session writer identity is invalid")
	}
	if nextSequence == 0 {
		return nil, fmt.Errorf("session writer next sequence is invalid")
	}
	if clock == nil {
		return nil, fmt.Errorf("session writer clock is required")
	}
	writer := &JournalWriter{
		file: file, identity: identity, clock: clock,
		requests: make(chan writerRequest, 64), closeReq: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	go writer.run(nextSequence)
	return writer, nil
}

// AppendBatch 编码并 durable 追加一个全有或全无恢复语义的 batch。
func (writer *JournalWriter) AppendBatch(ctx context.Context, drafts []RecordDraft) ([]Record, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if len(drafts) == 0 || len(drafts) > math.MaxUint32 {
		return nil, fmt.Errorf("session batch size is invalid")
	}
	prepared := make([]preparedDraft, 0, len(drafts))
	for _, draft := range drafts {
		value, err := prepareDraft(draft)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, value)
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
	select {
	case writer.requests <- request:
		writer.mu.Unlock()
	case <-ctx.Done():
		writer.mu.Unlock()
		return nil, fmt.Errorf("session append cancelled: %w", ctx.Err())
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
	if writer == nil {
		return nil
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	writer.mu.Lock()
	if !writer.closing {
		writer.closing = true
		writer.closeReq <- struct{}{}
	}
	done := writer.done
	writer.mu.Unlock()
	select {
	case <-done:
		return writer.closeErr
	case <-ctx.Done():
		return fmt.Errorf("session writer close cancelled: %w", ctx.Err())
	}
}

func (writer *JournalWriter) run(nextSequence uint64) {
	defer close(writer.done)
	for {
		select {
		case request := <-writer.requests:
			if writer.poisoned.Load() {
				request.response <- appendResponse{err: fmt.Errorf("session writer is poisoned")}
				continue
			}
			records, err := writer.append(nextSequence, request.drafts)
			if err != nil {
				writer.poisoned.Store(true)
				request.response <- appendResponse{err: err}
				continue
			}
			nextSequence += uint64(len(records))
			request.response <- appendResponse{records: records}
		case <-writer.closeReq:
			for {
				select {
				case request := <-writer.requests:
					if writer.poisoned.Load() {
						request.response <- appendResponse{err: fmt.Errorf("session writer is poisoned")}
						continue
					}
					records, err := writer.append(nextSequence, request.drafts)
					if err != nil {
						writer.poisoned.Store(true)
						request.response <- appendResponse{err: err}
						continue
					}
					nextSequence += uint64(len(records))
					request.response <- appendResponse{records: records}
				default:
					writer.closeErr = writer.finish()
					return
				}
			}
		}
	}
}

func (writer *JournalWriter) append(nextSequence uint64, drafts []preparedDraft) ([]Record, error) {
	batchSize := uint32(len(drafts))
	batchID := nextSequence
	var buffer bytes.Buffer
	records := make([]Record, 0, len(drafts))
	for index, draft := range drafts {
		record := Record{
			SchemaVersion: EnvelopeVersion, PayloadVersion: draft.descriptor.Version,
			ReplayRequirement: draft.descriptor.Requirement, Sequence: nextSequence + uint64(index),
			Timestamp: writer.clock().UTC(), SessionID: writer.identity.SessionID,
			ThreadID: writer.identity.ThreadID, ParentThreadID: domain.ThreadID(draft.parentThreadID),
			TurnID: domain.TurnID(draft.turnID), EventKind: draft.descriptor.Kind,
			BatchID: batchID, BatchIndex: uint32(index), BatchSize: batchSize,
			Payload: append(json.RawMessage(nil), draft.payload...),
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
		return nil, fmt.Errorf("append session batch: %w", err)
	}
	if written != buffer.Len() {
		return nil, fmt.Errorf("append session batch: %w", io.ErrShortWrite)
	}
	if err := writer.file.Sync(); err != nil {
		return nil, fmt.Errorf("sync session batch: %w", err)
	}
	return cloneRecords(records), nil
}

func (writer *JournalWriter) finish() error {
	syncErr := writer.file.Sync()
	closeErr := writer.file.Close()
	if writer.poisoned.Load() {
		return errors.Join(fmt.Errorf("session writer is poisoned"), syncErr, closeErr)
	}
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("close session writer: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

func prepareDraft(draft RecordDraft) (preparedDraft, error) {
	descriptor, err := descriptorForDraft(draft)
	if err != nil {
		return preparedDraft{}, err
	}
	payload, err := codec.MarshalStable(draft.Payload)
	if err != nil {
		return preparedDraft{}, fmt.Errorf("marshal session payload: %w", err)
	}
	if len(payload) > MaxRecordBytes {
		return preparedDraft{}, fmt.Errorf("session payload exceeds size limit")
	}
	return preparedDraft{
		descriptor: descriptor, parentThreadID: string(draft.ParentThreadID),
		turnID: string(draft.TurnID), payload: append(json.RawMessage(nil), payload...),
	}, nil
}

func cloneRecords(records []Record) []Record {
	cloned := make([]Record, len(records))
	for index, record := range records {
		cloned[index] = cloneRecord(record)
	}
	return cloned
}
