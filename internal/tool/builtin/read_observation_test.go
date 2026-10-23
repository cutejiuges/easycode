//go:build darwin || linux

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestReadObservationCompletenessAndExactBytes(t *testing.T) {
	for _, test := range []struct {
		name, content string
		offset, limit int
		complete      bool
	}{
		{"empty", "", 1, 2000, true}, {"crlf", "中文\r\nlast", 1, 2000, true},
		{"offset", "a\nb", 2, 2000, false}, {"range", "a\nb", 1, 1, false},
		{"long", strings.Repeat("界", 2001), 1, 2000, false},
		{"budget", strings.Repeat(strings.Repeat("x", 1999)+"\n", 140), 1, 2000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			executor, _ := openTestExecutor(t, root)
			invocation := invocationFor(t, "a", test.offset, test.limit)
			e := executor.Execute(context.Background(), invocation)
			if e.Validate() != nil || (e.Observation().Kind() == tool.ReadObservationComplete) != test.complete {
				t.Fatalf("观察完整性错误: %#v", e)
			}
			if test.complete && e.Observation().Evidence().ContentDigest() != tool.NewContentDigest([]byte(test.content)) {
				t.Fatal("摘要未绑定原始 bytes")
			}
			lines := splitTextLines(test.content)
			want := tool.RenderReadSuccess(invocation, "a", selectLines(lines, test.offset, test.limit), len(lines))
			if e.Result().Preview().Text() != want.Preview().Text() || e.Result().ReadMetadata() != want.ReadMetadata() {
				t.Fatal("观察改变了既有模型结果")
			}
		})
	}
}

func TestReadObservationRejectsReadTimeChanges(t *testing.T) {
	for _, change := range []string{"same-size", "mode", "unlink", "fd-closed"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a")
			if err := os.WriteFile(path, []byte("base"), 0o600); err != nil {
				t.Fatal(err)
			}
			executor, _ := openTestExecutor(t, root)
			executor.afterRead = func(file *os.File) {
				var err error
				switch change {
				case "same-size":
					err = os.WriteFile(path, []byte("next"), 0o600)
				case "mode":
					err = os.Chmod(path, 0o644)
				case "unlink":
					err = os.Remove(path)
					if err == nil {
						err = os.WriteFile(path, []byte("next"), 0o600)
					}
				case "fd-closed":
					err = file.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			e := executor.Execute(context.Background(), invocationFor(t, "a", 1, 2000))
			if e.Observation().Kind() != tool.ReadObservationUnavailable {
				t.Fatal("变化的文件产生了完整证据")
			}
		})
	}
}
