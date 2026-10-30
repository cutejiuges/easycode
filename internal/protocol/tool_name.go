package protocol

// ToolName 是独立于 Provider wire 名称的七种产品能力。
type ToolName string

const (
	ToolRead         ToolName = "Read"
	ToolGlob         ToolName = "Glob"
	ToolGrep         ToolName = "Grep"
	ToolPatch        ToolName = "Patch"
	ToolWrite        ToolName = "Write"
	ToolShell        ToolName = "Shell"
	ToolReadArtifact ToolName = "ReadArtifact"
)

// Valid 只接受实际实现且具有消费者的能力名称。
func (name ToolName) Valid() bool {
	switch name {
	case ToolRead, ToolGlob, ToolGrep, ToolPatch, ToolWrite, ToolShell, ToolReadArtifact:
		return true
	default:
		return false
	}
}
