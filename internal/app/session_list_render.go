package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"easycode/internal/session/catalog"
)

func formatSessionList(query catalog.ListQuery, result catalog.ListResult) string {
	var output strings.Builder
	if query.Scope == catalog.ListCurrentDirectory {
		fmt.Fprintf(&output, "Sessions in current directory (most recently updated first)\nDirectory: %s\n\n", sessionListField(query.CreationCWD, 512))
	} else {
		output.WriteString("Sessions across all directories (most recently updated first)\n\n")
	}
	if len(result.Entries) == 0 {
		if query.Scope == catalog.ListCurrentDirectory {
			output.WriteString("No indexed sessions found in current directory.\nTry: easycode sessions list --all\n")
		} else {
			output.WriteString("No indexed sessions found across all directories.\n")
		}
		return output.String()
	}
	for _, entry := range result.Entries {
		fmt.Fprintf(&output, "%s\n  Updated: %s\n  Provider: %s / %s | Model: %s\n", entry.ThreadID, entry.UpdatedAt.UTC().Format(time.RFC3339Nano), entry.ProviderFamily, sessionListField(entry.ProviderWire, 256), sessionListField(entry.Model, 256))
		if query.Scope == catalog.ListAllDirectories {
			fmt.Fprintf(&output, "  Directory: %s\n", sessionListField(entry.CreationCWD, 512))
		}
		output.WriteByte('\n')
	}
	if result.HasMore {
		if query.Limit < catalog.MaximumListLimit {
			fmt.Fprintf(&output, "More indexed sessions are available. Increase --limit (maximum %d).\n\n", catalog.MaximumListLimit)
		} else {
			output.WriteString("More indexed sessions are available; the current maximum is 200.\n\n")
		}
	}
	output.WriteString("Resume: easycode --resume <thread-id>\nUse matching provider, wire and model settings. Resume uses your current directory.\n")
	return output.String()
}

// sessionListField 按转义后的码点预算输出，保留完整转义单元。
func sessionListField(value string, limit int) string {
	var output strings.Builder
	count := 0
	for offset, r := range value {
		unit := string(r)
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\u061c' || r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			quoted := strconv.QuoteRune(r)
			unit = quoted[1 : len(quoted)-1]
		} else if r == '\\' {
			unit = `\\`
		}
		length := utf8.RuneCountInString(unit)
		_, size := utf8.DecodeRuneInString(value[offset:])
		remaining := offset+size < len(value)
		budget := limit
		if remaining {
			budget--
		}
		if count+length > budget {
			output.WriteRune('…')
			break
		}
		output.WriteString(unit)
		count += length
	}
	return output.String()
}
