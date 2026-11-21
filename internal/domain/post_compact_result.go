package domain

// PostCompactOverflowBytes 表示观测量已超过捕获上限，避免持久化无界计数。
const PostCompactOverflowBytes = MaxHookOutputBytes + 1

func (r HookRunResult) validPostCompactResult() bool {
	if r.Origin != "execution" || r.StdoutBytes > PostCompactOverflowBytes || r.StderrBytes > PostCompactOverflowBytes {
		return false
	}
	overflow := r.StdoutBytes == PostCompactOverflowBytes || r.StderrBytes == PostCompactOverflowBytes
	switch r.Code {
	case HookOutcomeUncertain:
		return !r.CleanupConfirmed
	case HookCancelled, HookTimedOut:
		return true
	case HookOutputLimit:
		return overflow
	case HookOK, HookExitFailed, HookStartFailed, HookInputLimit, HookIOFailed, HookUnsupported, HookSandboxUnavailable:
		return !overflow
	default:
		return false
	}
}
