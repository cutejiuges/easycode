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
	journalLeaseClosing
	journalLeaseClosed
)

// JournalLease 持有一个 thread journal 的进程级独占所有权。
type JournalLease struct {
	mu        sync.Mutex
	file      *os.File
	threadID  domain.ThreadID
	state     journalLeaseState
	ioDone    chan struct{}
	poisoned  bool
	repair    *repairTicket
	closeDone chan struct{}
	closeErr  error
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
	case journalLeaseTransferred:
		lease.mu.Unlock()
		return errJournalLeaseTransferred
	case journalLeaseClosing, journalLeaseClosed:
		done := lease.closeDone
		lease.mu.Unlock()
		<-done
		lease.mu.Lock()
		err := lease.closeErr
		lease.mu.Unlock()
		return err
	case journalLeaseActive:
		lease.state = journalLeaseClosing
		lease.closeDone = make(chan struct{})
		ioDone, file := lease.ioDone, lease.file
		lease.mu.Unlock()
		if ioDone != nil {
			<-ioDone
		}
		err := file.Close()
		if err != nil {
			err = fmt.Errorf("close session journal lease: %w", err)
		}
		lease.mu.Lock()
		lease.file = nil
		lease.state = journalLeaseClosed
		lease.closeErr = err
		close(lease.closeDone)
		lease.mu.Unlock()
		return err
	default:
		lease.mu.Unlock()
		return fmt.Errorf("session journal lease state is invalid")
	}
}

func (lease *JournalLease) transfer() (*os.File, error) {
	if lease == nil {
		return nil, fmt.Errorf("session journal lease is required")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.ioDone != nil || lease.poisoned || lease.repair != nil {
		return nil, fmt.Errorf("session journal lease is not ready for writer")
	}
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

// beginIO 在内存中取得一次 handle 操作所有权，禁止关闭或转交跨越它。
func (lease *JournalLease) beginIO() (*os.File, domain.ThreadID, error) {
	if lease == nil {
		return nil, "", fmt.Errorf("session journal lease is required")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.state != journalLeaseActive || lease.file == nil || lease.ioDone != nil || lease.poisoned {
		return nil, "", fmt.Errorf("session journal lease is unavailable")
	}
	lease.ioDone = make(chan struct{})
	return lease.file, lease.threadID, nil
}

func (lease *JournalLease) endIO() {
	lease.mu.Lock()
	done := lease.ioDone
	lease.ioDone = nil
	close(done)
	lease.mu.Unlock()
}
