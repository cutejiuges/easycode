package tool

import "errors"

// PatchResultMetadata 保存不含文件正文、绝对路径和临时文件名的Patch结果摘要。
type PatchResultMetadata struct {
	relativePath     string
	replacementCount int
	addedLines       int
	removedLines     int
}

// NewPatchResultMetadata 构造Patch结果元数据。
func NewPatchResultMetadata(relativePath string, replacementCount int, addedLines int, removedLines int) (PatchResultMetadata, error) {
	metadata := PatchResultMetadata{relativePath: relativePath, replacementCount: replacementCount, addedLines: addedLines, removedLines: removedLines}
	if err := metadata.Validate(); err != nil {
		return PatchResultMetadata{}, err
	}
	return metadata, nil
}

func (metadata PatchResultMetadata) RelativePath() string  { return metadata.relativePath }
func (metadata PatchResultMetadata) ReplacementCount() int { return metadata.replacementCount }
func (metadata PatchResultMetadata) AddedLines() int       { return metadata.addedLines }
func (metadata PatchResultMetadata) RemovedLines() int     { return metadata.removedLines }

// Validate 验证相对路径和确定计数。
func (metadata PatchResultMetadata) Validate() error {
	if validateSearchPath(metadata.relativePath) != nil || metadata.relativePath == "" || metadata.replacementCount < 1 || metadata.replacementCount > MaxPatchReplacements || metadata.addedLines < 0 || metadata.removedLines < 0 {
		return errors.New("patch result metadata is invalid")
	}
	return nil
}

func (metadata PatchResultMetadata) zero() bool {
	return metadata == (PatchResultMetadata{})
}
