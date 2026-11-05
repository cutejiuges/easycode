package localfile

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadBindsPermissionsAndConsistencyToDescriptor(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		if _, err := Read(context.Background(), "/file", 100, Options{}); err != Unsupported {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	p := filepath.Join(root, "hooks.json")
	if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	o := Options{RejectWritableFrom: root, Consistent: true}
	if b, err := Read(context.Background(), p, 2, o); err != nil || string(b) != "{}" {
		t.Fatal(string(b), err)
	}
	if _, err := Read(context.Background(), p, 1, o); err != LimitExceeded {
		t.Fatal("limit not enforced", err)
	}
	if err := os.Chmod(p, 0660); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(context.Background(), p, 10, o); err != Unsafe {
		t.Fatal("writable file accepted", err)
	}
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	o.AfterRead = func() {
		if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Read(context.Background(), p, 20, o); err != Changed {
		t.Fatal("changed file accepted", err)
	}
	o.AfterRead = nil
	fired := false
	o.BeforeOpen = func(current string) {
		if current == SystemPath(p) && !fired {
			fired = true
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "secret"), p); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := Read(context.Background(), p, 20, o); err == nil || !fired {
		t.Fatal("swapped symlink accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(cancelled, p, 20, o); err != context.Canceled {
		t.Fatal("cancel not observed", err)
	}
}
