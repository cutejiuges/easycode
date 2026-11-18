package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func TestPreviewLeaseProcess(t *testing.T) {
	if os.Getenv("EASYCODE_PREVIEW_LEASE_CHILD") != "1" {
		return
	}
	root := os.Getenv("EASYCODE_PREVIEW_ROOT")
	thread, err := domain.ParseThreadID(os.Getenv("EASYCODE_PREVIEW_THREAD"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Open(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	fmt.Fprintln(os.Stdout, "ready")
	io.Copy(io.Discard, os.Stdin)
}
