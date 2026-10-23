package builtin

import (
	"fmt"
	"strings"
	"unicode"

	"easycode/internal/tool"
)

func buildWritePreview(input tool.WriteInput, operation tool.WriteOperation, base []byte, directories []string) (string, error) {
	var raw string
	if operation == tool.WriteCreate && input.Content() == "" {
		raw = "Empty file (0 bytes, 0 lines)\n"
	} else {
		diff, _, err := buildPatchDiff(input.FilePath(), base, []byte(input.Content()), 1)
		if err != nil {
			return "", writeError("write_preview_too_large")
		}
		raw = diff
	}
	preview := visibleWriteText(raw)
	summary, _ := tool.NewWriteResultMetadata(input.FilePath(), operation, len(input.Content()), tool.WriteLineCount(input.Content()), len(directories))
	budget := tool.WritePresentationBytes(preview, summary, directories)
	if budget > tool.MaxApprovalDiffBytes {
		return "", writeError("write_preview_too_large")
	}
	return preview, nil
}

// visibleWriteText 让终端控制、BOM 和回车可见；仅转换展示，不触碰最终 bytes。
func visibleWriteText(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r == '\n':
			out.WriteRune(r)
		case r == '\\':
			out.WriteString(`\\`)
		case r == '\r':
			out.WriteString(`\r`)
		case r == '\t':
			out.WriteString(`\t`)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&out, `\u{%04X}`, r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
