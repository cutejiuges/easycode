package runtime

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

// HookRunner 只接收已经 durable started 的检查，不接触 Provider wire。
type HookRunner interface {
	Preflight(context.Context) domain.HookCode
	Run(context.Context, domain.HookRunIdentity, domain.HookDeclaration, domain.HookInput, time.Duration) domain.HookRunResult
}
type hookTrustWait struct {
	request   protocol.HookTrustRequestedPayload
	turn      domain.TurnID
	cancelled <-chan struct{}
	decision  protocol.HookTrustDecision
	resolved  bool
	done      chan struct{}
}
type hookState struct {
	mu           sync.Mutex
	snapshot     domain.HookSnapshot
	runner       HookRunner
	workspace    string
	availability domain.HookCode
	trust        map[string]protocol.HookTrustDecision
	wait         *hookTrustWait
	planned      map[tool.InvocationID]domain.HookPlan
	completed    map[tool.InvocationID]tool.InvocationResult
	now          func() time.Time
}

// InstallHooks 在离线恢复之后、宿主 Start 之前安装未来调用的冻结来源。
func (r *Runtime) InstallHooks(snapshot domain.HookSnapshot, runner HookRunner, workspace string, availability domain.HookCode) error {
	if r.active.Load() || runner == nil || !filepath.IsAbs(workspace) || !availability.Valid() {
		return fault.New(fault.CodeInvalidConfiguration, "invalid hook installation")
	}
	s, err := domain.NewHookSnapshot(snapshot.Declarations(), snapshot.Diagnostics())
	if err != nil {
		return err
	}
	r.hooks.mu.Lock()
	defer r.hooks.mu.Unlock()
	r.hooks.snapshot = s
	r.hooks.runner = runner
	r.hooks.workspace = workspace
	r.hooks.availability = availability
	r.hooks.trust = make(map[string]protocol.HookTrustDecision)
	r.hooks.planned = make(map[tool.InvocationID]domain.HookPlan)
	r.hooks.completed = make(map[tool.InvocationID]tool.InvocationResult)
	r.hooks.now = time.Now
	return nil
}

// HookEntries 返回本地只读投影，不读取来源或执行能力检查。
func (r *Runtime) HookEntries() []domain.HookListEntry {
	r.hooks.mu.Lock()
	defer r.hooks.mu.Unlock()
	var out []domain.HookListEntry
	for _, d := range r.hooks.snapshot.Declarations() {
		entry := domain.HookListEntry{Declaration: d, State: "untrusted", Code: domain.HookOK}
		if r.hooks.availability != domain.HookOK {
			entry.State = "unsupported"
			entry.Code = r.hooks.availability
		} else {
			switch r.hooks.trust[d.Reference.Key()] {
			case protocol.HookTrustSession:
				entry.State = "trusted"
			case protocol.HookTrustDeny:
				entry.State = "denied"
			}
		}
		out = append(out, entry)
	}
	for _, d := range r.hooks.snapshot.Diagnostics() {
		out = append(out, domain.HookListEntry{Declaration: domain.HookDeclaration{Reference: domain.HookReference{Source: d.Source}}, State: "invalid", Code: d.Code})
	}
	return out
}

func (r *Runtime) resetHookTurn() {
	r.hooks.mu.Lock()
	r.hooks.planned = make(map[tool.InvocationID]domain.HookPlan)
	r.hooks.completed = make(map[tool.InvocationID]tool.InvocationResult)
	r.hooks.mu.Unlock()
}
func (r *Runtime) rememberHookResult(result tool.InvocationResult) {
	r.hooks.mu.Lock()
	if _, ok := r.hooks.planned[result.InvocationID()]; ok {
		r.hooks.completed[result.InvocationID()] = result
	}
	r.hooks.mu.Unlock()
}
