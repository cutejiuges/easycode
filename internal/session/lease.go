package session

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"easycode/internal/domain"
)

var (
	errJournalBusy             = errors.New("session journal is busy")
	errJournalLeaseClosed      = errors.New("session journal lease is closed")
	errJournalLeaseTransferred = errors.New("session journal lease ownership was transferred")
)

type journalLeaseState uint8

const (
	journalLeaseActive journalLeaseState = iota
	journalLeaseTransferred
	journalLeaseClosed
)

// JournalLease 持有一个 thread journal 的进程级独占所有权。
type JournalLease struct {
	mu       sync.Mutex
	file     *os.File
	threadID domain.ThreadID
	state    journalLeaseState
}

func newJournalLease(file *os.File, threadID domain.ThreadID) *JournalLease {
	return &JournalLease{file: file, threadID: threadID, state: journalLeaseActive}
}

// IsJournalBusy 判断错误是否表示已有协作进程持有 journal。
func IsJournalBusy(err error) bool {
	return errors.Is(err, errJournalBusy)
}

// Close 关闭仍由 lease 持有的 handle；重复关闭是幂等的。
func (lease *JournalLease) Close() error {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	switch lease.state {
	case journalLeaseClosed:
		lease.mu.Unlock()
		return nil
	case journalLeaseTransferred:
		lease.mu.Unlock()
		return errJournalLeaseTransferred
	case journalLeaseActive:
		file := lease.file
		lease.file = nil
		lease.state = journalLeaseClosed
		lease.mu.Unlock()
		if err := file.Close(); err != nil {
			return fmt.Errorf("close session journal lease: %w", err)
		}
		return nil
	default:
		lease.mu.Unlock()
		return fmt.Errorf("session journal lease state is invalid")
	}
}

func (lease *JournalLease) borrow() (*os.File, domain.ThreadID, error) {
	if lease == nil {
		return nil, "", fmt.Errorf("session journal lease is required")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.state != journalLeaseActive || lease.file == nil {
		if lease.state == journalLeaseTransferred {
			return nil, "", errJournalLeaseTransferred
		}
		return nil, "", errJournalLeaseClosed
	}
	return lease.file, lease.threadID, nil
}

func (lease *JournalLease) transfer() (*os.File, error) {
	if lease == nil {
		return nil, fmt.Errorf("session journal lease is required")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.state != journalLeaseActive || lease.file == nil {
		if lease.state == journalLeaseTransferred {
			return nil, errJournalLeaseTransferred
		}
		return nil, errJournalLeaseClosed
	}
	file := lease.file
	lease.file = nil
	lease.state = journalLeaseTransferred
	return file, nil
}
