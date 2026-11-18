package domain

import "encoding/json"

// 以下私有映射只在 HookFinished 的一次编码内复用目标字节，字段顺序与原值保持一致。
type hookFinishedIdentity struct {
	Event      HookEvent       `json:"event"`
	Target     json.RawMessage `json:"target"`
	PlanDigest string          `json:"plan_digest"`
	Ordinal    int             `json:"ordinal"`
	RunID      string          `json:"run_id"`
}

func finishedIdentity(identity HookRunIdentity, target []byte) hookFinishedIdentity {
	return hookFinishedIdentity{identity.Event, target, identity.PlanDigest, identity.Ordinal, identity.RunID}
}

type hookFinishedResult struct {
	Identity         hookFinishedIdentity `json:"identity"`
	Status           HookOutcome          `json:"status"`
	Code             HookCode             `json:"code"`
	ExitKnown        bool                 `json:"exit_known"`
	ExitCode         int                  `json:"exit_code"`
	TimedOut         bool                 `json:"timed_out"`
	Cancelled        bool                 `json:"cancelled"`
	StdoutBytes      int64                `json:"stdout_bytes"`
	StderrBytes      int64                `json:"stderr_bytes"`
	CleanupConfirmed bool                 `json:"cleanup_confirmed"`
	Origin           string               `json:"origin"`
}

type hookFinishedFeedback struct {
	Identity    hookFinishedIdentity `json:"identity"`
	Reference   HookReference        `json:"reference"`
	Kind        HookFeedbackKind     `json:"kind"`
	Content     string               `json:"content"`
	Code        HookCode             `json:"code"`
	BudgetDelta int                  `json:"budget_delta"`
}
