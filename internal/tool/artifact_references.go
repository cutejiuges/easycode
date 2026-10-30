package tool

import (
	"easycode/internal/domain"
	"errors"
	"sync"
)

// ArtifactReferences 是一个线程内已提交引用的授权集合，锁内仅访问内存。
type ArtifactReferences struct {
	mu     sync.RWMutex
	thread domain.ThreadID
	items  map[ArtifactID]ArtifactDescriptor
}

// NewArtifactReferences 从已验证的恢复投影复制引用，不访问文件。
func NewArtifactReferences(thread domain.ThreadID, descriptors []ArtifactDescriptor) (*ArtifactReferences, error) {
	if !thread.Valid() {
		return nil, errors.New("invalid artifact reference thread")
	}
	r := &ArtifactReferences{thread: thread, items: make(map[ArtifactID]ArtifactDescriptor)}
	for _, d := range descriptors {
		if d.Validate() != nil || d.ThreadID() != thread {
			return nil, errors.New("invalid restored artifact reference")
		}
		if old, ok := r.items[d.ID()]; ok && old != d {
			return nil, errors.New("conflicting artifact reference")
		}
		r.items[d.ID()] = d
	}
	return r, nil
}

// AuthorizeCommitted 只能由 result 已成功 Sync 的 owner 调用。
func (r *ArtifactReferences) AuthorizeCommitted(result InvocationResult) error {
	if r == nil || result.Validate() != nil {
		return errors.New("invalid committed artifact result")
	}
	var descriptors []ArtifactDescriptor
	for _, d := range result.ArtifactDescriptors() {
		if d.ThreadID() != r.thread {
			return errors.New("artifact thread mismatch")
		}
		descriptors = append(descriptors, d)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range descriptors {
		if old, ok := r.items[d.ID()]; ok && old != d {
			return errors.New("conflicting artifact reference")
		}
	}
	for _, d := range descriptors {
		r.items[d.ID()] = d
	}
	return nil
}

// ValidatePending 在持久化前验证引用归属与冲突，但不授予读取权限。
func (r *ArtifactReferences) ValidatePending(result InvocationResult) error {
	if r == nil || result.Validate() != nil {
		return errors.New("invalid pending artifact result")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var previous ArtifactID
	for _, d := range result.ArtifactDescriptors() {
		if d.ThreadID() != r.thread || d.ID() == previous {
			return errors.New("artifact reference mismatch")
		}
		if old, exists := r.items[d.ID()]; exists && old != d {
			return errors.New("conflicting artifact reference")
		}
		previous = d.ID()
	}
	return nil
}

// Lookup 对未提交、未知和其他线程 ID 返回相同的不可用状态。
func (r *ArtifactReferences) Lookup(id ArtifactID) (ArtifactDescriptor, bool) {
	if r == nil || !id.Valid() {
		return ArtifactDescriptor{}, false
	}
	r.mu.RLock()
	d, ok := r.items[id]
	r.mu.RUnlock()
	return d, ok
}

// ArtifactDescriptors 仅投影当前能力允许且已经冻结的引用，不访问保存对象。
func (result InvocationResult) ArtifactDescriptors() []ArtifactDescriptor {
	var savings []ArtifactSaving
	switch result.capability {
	case CapabilityExec:
		savings = []ArtifactSaving{result.execMetadata.StdoutSaving(), result.execMetadata.StderrSaving()}
	case CapabilityGlob, CapabilityGrep:
		savings = []ArtifactSaving{result.SearchSaving()}
	}
	var descriptors []ArtifactDescriptor
	for _, saving := range savings {
		if d, ok := saving.Descriptor(); ok {
			descriptors = append(descriptors, d)
		}
	}
	return descriptors
}
