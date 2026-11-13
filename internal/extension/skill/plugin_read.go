package skill

import (
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/extension"
	"easycode/internal/extension/localfile"
	"errors"
	"path"
)

type pluginBinding struct {
	contribution extension.Contribution
	directory    string
	directoryID  localfile.Identity
	documentID   localfile.Identity
	sidecar      sidecarBinding
}

func pluginRevision(binding pluginBinding, metadata domain.SkillMetadata, digest string) string {
	value, _ := codec.MarshalStable(struct {
		Contribution, Directory, Name, Document, Sidecar string
		Present, User, Disabled                          bool
	}{binding.contribution.Revision, binding.directory, metadata.Name, digest, binding.sidecar.digest, binding.sidecar.present, metadata.UserInvocable, metadata.DisableModelInvocation})
	return domain.SkillDigest(string(value))
}

func (l *Local) loadPlugin(ctx context.Context, e localEntry) (localEntry, document, extension.SourceHandle, error) {
	binding := e.plugin
	handle, err := binding.contribution.Opener.Open(ctx, binding.directory)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ErrorCode(err) == "skill_unavailable" || ErrorCode(err) == "skill_source_busy" {
			return e, document{}, nil, err
		}
		return e, document{}, nil, failure("skill_source_changed")
	}
	success := false
	defer func() {
		if !success {
			_ = handle.Close()
		}
	}()
	main, err := handle.Read(ctx, "SKILL.md", domain.MaxSkillDocumentBytes)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return e, document{}, nil, err
	}
	if err != nil || handle.Identity() != binding.directoryID || main.Identity != binding.documentID || domain.SkillDigest(string(main.Bytes)) != e.digest {
		if ctx.Err() != nil {
			return e, document{}, nil, ctx.Err()
		}
		return e, document{}, nil, failure("skill_source_changed")
	}
	sidecar, _, err := readSidecar(ctx, handle, e.view.Source.Kind, domain.MaxSkillDocumentBytes)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return e, document{}, nil, err
	}
	if err != nil || sidecar != binding.sidecar {
		if ctx.Err() != nil {
			return e, document{}, nil, ctx.Err()
		}
		return e, document{}, nil, failure("skill_source_changed")
	}
	doc, err := parseDocument(e.view.Source, path.Base(binding.directory), string(main.Bytes))
	if err != nil {
		return e, document{}, nil, failure("skill_source_changed")
	}
	// 来源验证后继续使用冻结的有效策略，主文档原值不能覆盖副文件限制。
	doc.metadata = e.view.Metadata
	success = true
	return e, doc, handle, nil
}

// PluginSummaries 返回最终纳入结果的独立副本，不执行来源读取。
func (l *Local) PluginSummaries() []extension.PluginSummary {
	out := append([]extension.PluginSummary(nil), l.plugins...)
	for i := range out {
		out[i].Diagnostics = append([]domain.PluginDiagnostic(nil), out[i].Diagnostics...)
	}
	return out
}
