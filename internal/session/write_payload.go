package session

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/tool"
)

// WriteInputPayload 保存模型请求的必要文本，不添加磁盘快照。
type WriteInputPayload struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// UnmarshalJSON 对 Session 内嵌参数应用同一完整 strict decoder。
func (payload *WriteInputPayload) UnmarshalJSON(data []byte) error {
	input, err := tool.DecodeWriteInput(data)
	if err != nil {
		return err
	}
	payload.FilePath, payload.Content = input.FilePath(), input.Content()
	return nil
}

// WriteResultMetadataPayload 只保存冻结的路径、操作和结果计数。
type WriteResultMetadataPayload struct {
	RelativePath       string              `json:"relative_path"`
	Operation          tool.WriteOperation `json:"operation"`
	Bytes              int                 `json:"bytes"`
	Lines              int                 `json:"lines"`
	CreatedDirectories int                 `json:"created_directories"`
}

// UnmarshalJSON 拒绝缺失或 null 计数，避免以零值伪装空文件成功。
func (payload *WriteResultMetadataPayload) UnmarshalJSON(data []byte) error {
	if err := codec.ValidateObjectFields(data, "relative_path", "operation", "bytes", "lines", "created_directories"); err != nil {
		return err
	}
	var wire struct {
		RelativePath       *string              `json:"relative_path"`
		Operation          *tool.WriteOperation `json:"operation"`
		Bytes              *int                 `json:"bytes"`
		Lines              *int                 `json:"lines"`
		CreatedDirectories *int                 `json:"created_directories"`
	}
	if err := codec.UnmarshalStrict(data, &wire); err != nil {
		return fmt.Errorf("invalid Write result metadata")
	}
	if wire.RelativePath == nil || wire.Operation == nil || wire.Bytes == nil || wire.Lines == nil || wire.CreatedDirectories == nil {
		return fmt.Errorf("incomplete Write result metadata")
	}
	*payload = WriteResultMetadataPayload{RelativePath: *wire.RelativePath, Operation: *wire.Operation, Bytes: *wire.Bytes, Lines: *wire.Lines, CreatedDirectories: *wire.CreatedDirectories}
	return nil
}

func decodeWriteResult(payload ToolCallResultPayload, invocation tool.Invocation, preview tool.ModelPreview) (tool.InvocationResult, error) {
	if payload.WireKind != tool.WireStructured || payload.WriteMetadata == nil || payload.ReadMetadata != nil || payload.GlobMetadata != nil || payload.GrepMetadata != nil || payload.PatchMetadata != nil {
		return tool.InvocationResult{}, fmt.Errorf("invalid Write metadata union")
	}
	v, ok := invocation.Write()
	if !ok {
		return tool.InvocationResult{}, fmt.Errorf("write invocation is required")
	}
	m := payload.WriteMetadata
	metadata, err := tool.NewWriteResultMetadata(m.RelativePath, m.Operation, m.Bytes, m.Lines, m.CreatedDirectories)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return tool.NewWriteInvocationResult(v, payload.Status, payload.Code, preview, metadata)
}
