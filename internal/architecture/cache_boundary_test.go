package architecture_test

import (
	"strconv"
	"testing"
)

func TestWebSocketLibraryOwnedByTransport(t *testing.T) {
	repository := loadRepositorySource(t)
	for path, pkg := range repository.packages {
		for relative, file := range pkg.files {
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if value == "github.com/coder/websocket" && path != "easycode/internal/provider/transport" {
					t.Errorf("%s: websocket library must remain in transport", relative)
				}
			}
		}
	}
}
