package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
)

func TestFileCurrentFixturesRejectBeforeRepairAndNeverReadSources(t *testing.T) {
	fixtures, err := filepath.Glob("../session/testdata/current/files_*.jsonl")
	if err != nil || len(fixtures) != 22 {
		t.Fatal("missing fixed file fixtures", len(fixtures), err)
	}
	for _, file := range fixtures {
		t.Run(filepath.Base(file), func(t *testing.T) {
			original, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			root, path, thread := installPreviewFixture(t, original)
			data := append(append([]byte(nil), original...), []byte(`{"torn":`)...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			valid := strings.HasPrefix(filepath.Base(file), "files_input_")
			preview, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
			if cleanup || (err == nil) != valid {
				t.Fatal("preview shape validation wrong", err)
			}
			if valid && (len(preview.Messages) != 1 || preview.Messages[0].Files.Total() != 2 || preview.Messages[0].Message.Files.Len() != 0) {
				t.Fatal("preview retained full bodies or omitted source summary")
			}
			family := domain.ProviderOpenAI
			if strings.HasSuffix(file, "_anthropic.jsonl") {
				family = domain.ProviderAnthropic
			}
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"}}
			workspace := privateAppTempDir(t)
			r, err := openChatResources(context.Background(), cfg, root, string(thread), workspace)
			if (err == nil) != valid {
				if r != nil {
					_ = r.close(context.Background())
				}
				t.Fatal("restore shape validation wrong", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !valid {
				if !bytes.Equal(after, data) {
					t.Fatal("invalid native contract repaired journal")
				}
				return
			}
			if !r.repair.Repaired || r.history.Turns[0].Messages[0].Files.Len() != 2 {
				t.Fatal("valid interrupted input lost files")
			}
			if err := r.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			stable, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err = openChatResources(context.Background(), cfg, root, string(thread), workspace)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			again, _ := os.ReadFile(path)
			if !bytes.Equal(stable, again) {
				t.Fatal("second restore repeated recovery facts")
			}
		})
	}
}
