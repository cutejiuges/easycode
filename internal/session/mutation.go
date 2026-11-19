package session

import "errors"

// journalMutationUncertain 标记已接收写入后无法确认持久化结果的错误。
type journalMutationUncertain struct{ cause error }

func (err *journalMutationUncertain) Error() string { return err.cause.Error() }
func (err *journalMutationUncertain) Unwrap() error { return err.cause }

// IsJournalMutationUncertain 区分已接收写入的失败与接收前拒绝。
func IsJournalMutationUncertain(err error) bool {
	var uncertain *journalMutationUncertain
	return errors.As(err, &uncertain)
}
