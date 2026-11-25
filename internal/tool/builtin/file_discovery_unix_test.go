//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"easycode/internal/domain"
)

func TestFileDiscoveryCancellationAndIndexStopCloseEveryBorrow(t *testing.T) {
	root := t.TempDir()
	writeBuiltinFixture(t, filepath.Join(root, "a"), []byte("body"), 0o600)
	workspace := openFixtureWorkspace(t, root)
	defer workspace.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := workspace.DiscoverFileNames(ctx, func(paths []string, _ domain.FileDiscoveryReport) (bool, error) {
		if len(paths) > 0 {
			cancel()
		}
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("discovery ignored cancellation", err)
	}
	report, err := workspace.DiscoverFileNames(context.Background(), func([]string, domain.FileDiscoveryReport) (bool, error) { return false, nil })
	if err != nil || report.Issues&domain.FileDiscoveryIndexLimit == 0 {
		t.Fatal("index stop hidden", report, err)
	}
	if _, _, err = workspace.ReadWorkspaceFile(context.Background(), "a", domain.MaxFileSourceBytes); err != nil {
		t.Fatal("discovery released root capability", err)
	}
}
