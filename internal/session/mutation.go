package session

import "errors"

// journalMutationUncertain 标记已接收写入后无法确认持久化结果的错误。
type journalMutationUncertain struct{ cause error }

func (err *journalMutationUncertain) Error() string { return err.cause.Error() }
func (err *journalMutationUncertain) Unwrap() error { return err.cause }

// IsJournalMutationUncertain 判断文件变更开始后是否无法确认结果。
func IsJournalMutationUncertain(err error) bool {
	var uncertain *journalMutationUncertain
	return errors.As(err, &uncertain)
}
