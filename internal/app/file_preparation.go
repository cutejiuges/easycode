package app

import (
	"context"

	"easycode/internal/domain"
)

type workspaceFileReader interface {
	ReadWorkspaceFile(context.Context, string, int) (string, int, error)
}

// prepareWorkspaceFiles 复用已验证引用，按路径各读一次，再恢复原顺序。
func prepareWorkspaceFiles(ctx context.Context, refs []domain.FileReference, reader workspaceFileReader) (domain.FileAttachments, error) {
	if err := ctx.Err(); err != nil {
		return domain.FileAttachments{}, err
	}
	if len(refs) == 0 {
		return domain.FileAttachments{}, nil
	}
	if reader == nil {
		return domain.FileAttachments{}, domain.NewFileFailure(domain.FileUnavailable)
	}
	type group struct {
		path    string
		indices []int
	}
	groups := make([]group, 0, len(refs))
	indices := make(map[string]int, len(refs))
	for i, ref := range refs {
		index, found := indices[ref.Path()]
		if !found {
			index = len(groups)
			indices[ref.Path()] = index
			groups = append(groups, group{path: ref.Path()})
		}
		groups[index].indices = append(groups[index].indices, i)
	}
	attachments := make([]domain.FileAttachment, len(refs))
	consumed := 0
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return domain.FileAttachments{}, err
		}
		limit := min(domain.MaxFileSourceBytes, domain.MaxFileInputSourceBytes-consumed)
		content, count, err := reader.ReadWorkspaceFile(ctx, group.path, limit)
		if count < 0 || count > limit {
			return domain.FileAttachments{}, domain.NewFileFailure(domain.FileLimit)
		}
		consumed += count
		if err != nil {
			return domain.FileAttachments{}, err
		}
		if count != len(content) {
			return domain.FileAttachments{}, domain.NewFileFailure(domain.FileChanged)
		}
		for _, index := range group.indices {
			attachment, err := domain.FreezeFileAttachment(refs[index], content)
			if err != nil {
				return domain.FileAttachments{}, err
			}
			attachments[index] = attachment
		}
	}
	return domain.NewFileAttachments(attachments)
}
