package builtin

import (
	"context"
	"errors"
	"os"
	"testing"

	"easycode/internal/domain"
)

type fileUnsupportedHandle struct{ workspaceHandle }

func (fileUnsupportedHandle) openFile(string, workspaceHooks) (*os.File, error) {
	return nil, errWorkspaceUnsupported
}
func (fileUnsupportedHandle) openDirectory(string, workspaceHooks) (workspaceDirectory, error) {
	return nil, errWorkspaceUnsupported
}

func TestWorkspaceFileCapabilitiesFailClosedOnUnsupportedPlatform(t *testing.T) {
	w := &Workspace{handle: fileUnsupportedHandle{}}
	content, count, err := w.ReadWorkspaceFile(context.Background(), "a", domain.MaxFileSourceBytes)
	var failure *domain.FileFailure
	if content != "" || count != 0 || !errors.As(err, &failure) || failure.Kind() != domain.FilePlatformUnsupported {
		t.Fatal("unsupported file read did not fail closed", err)
	}
	_, err = w.DiscoverFileNames(context.Background(), func([]string, domain.FileDiscoveryReport) (bool, error) {
		t.Fatal("unsupported discovery published names")
		return false, nil
	})
	if !errors.As(err, &failure) || failure.Kind() != domain.FilePlatformUnsupported {
		t.Fatal("unsupported discovery did not fail closed", err)
	}
}
