package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// artifactReplay 按已验证结果的 durable 顺序绑定授权。语法校验完成后才报告首个引用错误。
type artifactReplay struct {
	references *tool.ArtifactReferences
	err        error
}

func newArtifactReplay(thread domain.ThreadID) artifactReplay {
	references, err := tool.NewArtifactReferences(thread, nil)
	if err != nil {
		return artifactReplay{err: replayCorrupt("artifact reference thread is invalid")}
	}
	return artifactReplay{references: references}
}

func (replay *artifactReplay) accept(result tool.InvocationResult) {
	if replay.err != nil {
		return
	}
	switch result.Capability() {
	case tool.CapabilityExec, tool.CapabilityGlob, tool.CapabilityGrep:
		if replay.references.ValidatePending(result) != nil || replay.references.AuthorizeCommitted(result) != nil {
			replay.err = replayCorrupt("artifact references conflict")
		}
	case tool.CapabilityReadArtifact:
		if page, ok := result.ArtifactPage(); ok {
			descriptor, exists := replay.references.Lookup(page.ID())
			if !exists || uint64(descriptor.SavedBytes()) != page.SavedBytes() || descriptor.Complete() != page.SourceComplete() {
				replay.err = replayCorrupt("artifact page has no matching committed reference")
			}
		}
	}
}
