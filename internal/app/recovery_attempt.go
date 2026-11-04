package app

// recoveryPhase 区分纯验证失败与可能已改变日志的恢复失败。
type recoveryPhase uint8

const (
	recoveryValidating recoveryPhase = iota
	recoveryMutating
	recoveryCleanupFailed
)

type recoveryAttempt struct{ phase recoveryPhase }

func (a *recoveryAttempt) mutating() {
	if a != nil && a.phase == recoveryValidating {
		a.phase = recoveryMutating
	}
}
func (a *recoveryAttempt) cleanup(err error) {
	if a != nil && err != nil {
		a.phase = recoveryCleanupFailed
	}
}
func (a *recoveryAttempt) fatal() bool { return a != nil && a.phase != recoveryValidating }
