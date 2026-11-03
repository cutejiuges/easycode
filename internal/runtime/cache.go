package runtime

import (
	"fmt"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

type cacheDiagnostics struct {
	snapshot    protocol.CacheSnapshotPayload
	previous    contextplan.CacheSources
	priorNative string
	priorPolicy string
	frozen      contextplan.CacheSources
}

func (runtime *Runtime) initialCacheSnapshot() (protocol.CacheSnapshotPayload, error) {
	plan, err := runtime.contextPlan()
	if err != nil {
		return protocol.CacheSnapshotPayload{}, err
	}
	return runtime.cacheSnapshotForPlan(plan)
}
func (runtime *Runtime) cacheSnapshotForPlan(plan contextplan.ContextPlan) (protocol.CacheSnapshotPayload, error) {
	sources, err := contextplan.NewCacheSources(plan)
	if err != nil {
		return protocol.CacheSnapshotPayload{}, err
	}
	policy, err := protocol.NewCachePolicyPayload(runtime.cachePolicy)
	if err != nil {
		return protocol.CacheSnapshotPayload{}, err
	}
	wire := "responses"
	if runtime.conversation.Family() == domain.ProviderAnthropic {
		wire = "messages"
	}
	baseline, reason := "fresh", "first_request"
	if runtime.restoredCacheBaseline {
		baseline = "restored_baseline"
		reason = "restored_baseline"
	}
	snapshot := protocol.CacheSnapshotPayload{Scope: "process", Baseline: baseline, Activity: "idle", Family: runtime.conversation.Family(), Wire: wire, Model: protocol.CacheModelIdentity(runtime.profile.Model()), Policy: policy, ChangeReason: reason}
	copyCacheSources(&snapshot, sources)
	return snapshot, snapshot.Validate()
}
func copyCacheSources(snapshot *protocol.CacheSnapshotPayload, sources contextplan.CacheSources) {
	snapshot.ContentFingerprint = sources.ContentFingerprint
	snapshot.StableFingerprint = sources.StableFingerprint
	for i, s := range sources.Sources {
		snapshot.Sources[i] = protocol.CacheSourcePayload{Kind: string(s.Kind), Present: s.Present, Revision: s.Revision, Fingerprint: s.Fingerprint}
	}
}
func (runtime *Runtime) freezeCacheSources(plan contextplan.ContextPlan) error {
	if runtime.cache.snapshot.Scope == "" {
		snapshot, err := runtime.cacheSnapshotForPlan(plan)
		if err != nil {
			return err
		}
		runtime.cache.snapshot = snapshot
	}
	sources, err := contextplan.NewCacheSources(plan)
	runtime.cache.frozen = sources
	return err
}
func (runtime *Runtime) sampleObservation(sample *provider.PreparedSample) (protocol.CacheObservationPayload, error) {
	receipt, err := sample.Receipt()
	if err != nil {
		return protocol.CacheObservationPayload{}, err
	}
	usage, err := sample.Usage()
	if err != nil {
		return protocol.CacheObservationPayload{}, err
	}
	return runtime.cacheObservation(receipt, usage, domain.RequestSample, "")
}
func (runtime *Runtime) cacheObservation(receipt domain.RequestReceipt, usage domain.SampleUsage, purpose domain.RequestPurpose, id domain.CompactionID) (protocol.CacheObservationPayload, error) {
	if err := receipt.Validate(); err != nil {
		return protocol.CacheObservationPayload{}, err
	}
	if receipt.Family != runtime.conversation.Family() || receipt.Purpose != purpose || receipt.Policy != runtime.cachePolicy {
		return protocol.CacheObservationPayload{}, fmt.Errorf("request receipt does not match frozen operation")
	}
	if err := runtime.cache.frozen.Validate(); err != nil {
		return protocol.CacheObservationPayload{}, err
	}
	payload, err := protocol.NewUsagePayload(usage)
	if err != nil {
		return protocol.CacheObservationPayload{}, err
	}
	read, known := usage.CacheRead().Value()
	o := protocol.CacheObservationPayload{Purpose: purpose, CompactionID: id, Usage: payload, ReadObserved: known && read > 0, RequestFingerprint: receipt.RequestFingerprint, NativeFingerprint: receipt.NativeFingerprint, Transport: receipt.Transport, Mode: receipt.Mode, Relation: receipt.Relation, Reason: receipt.Reason, Fallback: receipt.Fallback, Markers: receipt.Markers}
	if ratio, ok := domain.CacheReadRatio(usage); ok {
		o.ReadRatio = &ratio
	}
	return o, nil
}
func (runtime *Runtime) commitSampleObservation(observation protocol.CacheObservationPayload) {
	// 收据必须在 finalize 前读取；此处只接收已验证的值，不接触已解析对象。
	receipt := domain.RequestReceipt{Reason: observation.Reason, Relation: observation.Relation, NativeFingerprint: observation.NativeFingerprint}
	change := contextplan.CompareCacheSources(runtime.cache.previous, runtime.cache.frozen, runtime.cache.priorNative, receipt, runtime.restoredCacheBaseline && runtime.cache.previous.ContentFingerprint == "")
	snapshot := runtime.cache.snapshot
	snapshot.ChangeReason = change.Reason
	snapshot.ChangedSource = string(change.Source)
	snapshot.PolicyChanged = runtime.cache.priorPolicy != "" && runtime.cache.priorPolicy != snapshot.Policy.Fingerprint
	snapshot.UnexplainedReadDrop = false
	if previous := snapshot.LastSample; previous != nil && (change.Reason == "unchanged" || change.Reason == "history_appended") && !snapshot.PolicyChanged {
		old, oldKnown := previous.Usage.CacheRead.Value, previous.Usage.CacheRead.State == domain.UsageMetricKnown
		now, nowKnown := observation.Usage.CacheRead.Value, observation.Usage.CacheRead.State == domain.UsageMetricKnown
		snapshot.UnexplainedReadDrop = oldKnown && nowKnown && old != nil && now != nil && *old > 0 && *now == 0
	}
	snapshot.LastSample = &observation
	snapshot.LastFailure = ""
	copyCacheSources(&snapshot, runtime.cache.frozen)
	runtime.cache.snapshot = snapshot
	runtime.cache.previous = runtime.cache.frozen
	runtime.cache.priorNative = observation.NativeFingerprint
	runtime.cache.priorPolicy = snapshot.Policy.Fingerprint
}
func (state *agentLoopRunState) queryCache(request loopCommandRequest) {
	payload := state.cacheSnapshot
	if state.cacheSnapshotErr != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	if state.active != nil {
		payload.Activity = "turn"
		if state.active.compactionID.Valid() {
			payload.Activity = "compacting"
		}
	} else if state.compaction != nil {
		payload.Activity = "compacting"
	} else {
		payload.Activity = "idle"
	}
	event, err := protocol.NewCacheSnapshot(state.loop.runtime.sessionID, state.loop.runtime.threadID, payload)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionQueried)
	state.emitResult(request, result)
	item, _ := protocol.NewCorrelatedContextItem(request.command.RequestID(), event)
	state.loop.outputs <- item
	state.completeRequest(request.command.RequestID())
}

func (runtime *Runtime) publishCacheSnapshot() {
	if runtime.cachePublisher != nil {
		runtime.cachePublisher(runtime.cache.snapshot)
	}
}
