//go:build darwin || linux

package builtin

import (
	"bytes"
	"context"
	"easycode/internal/tool"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteCrashChild(t *testing.T) {
	boundary := os.Getenv("EASYCODE_WRITE_CRASH_BOUNDARY")
	if boundary == "" {
		return
	}
	root := os.Getenv("EASYCODE_WRITE_CRASH_ROOT")
	path := os.Getenv("EASYCODE_WRITE_CRASH_PATH")
	_, p, e, r := writeComponents(t, root)
	observation := tool.UnavailableReadObservation()
	if os.Getenv("EASYCODE_WRITE_CRASH_OVERWRITE") == "1" {
		observation = readObservation(t, r, path)
	}
	c, err := p.Prepare(context.Background(), writeCall(t, path, "committed\r\n中文"), observation)
	if err != nil {
		t.Fatal(err)
	}
	stop := func() {
		signal := os.NewFile(3, "publication-boundary")
		if _, err := signal.Write([]byte("ready")); err != nil {
			t.Fatal(err)
		}
		if err := signal.Close(); err != nil {
			t.Fatal(err)
		}
		var resume [1]byte
		if _, err := os.Stdin.Read(resume[:]); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash child unexpectedly resumed")
	}
	if boundary == "before" {
		e.hooks.beforePublish = stop
	} else {
		e.hooks.afterPublish = stop
	}
	e.Execute(context.Background(), c)
	t.Fatal("publication boundary was not reached")
}

func TestWriteProcessCrashAtPublicationBoundary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a", "new/pkg/a"} {
		for _, overwrite := range []bool{false, true} {
			for _, boundary := range []string{"before", "after"} {
				t.Run(fmt.Sprintf("%s/%t/%s", path, overwrite, boundary), func(t *testing.T) {
					root := t.TempDir()
					target := filepath.Join(root, path)
					if overwrite {
						if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					readSignal, writeSignal, err := os.Pipe()
					if err != nil {
						t.Fatal(err)
					}
					defer readSignal.Close()
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, executable, "-test.run=^TestWriteCrashChild$", "-test.timeout=15s")
					cmd.Env = append(os.Environ(), "EASYCODE_WRITE_CRASH_ROOT="+root, "EASYCODE_WRITE_CRASH_PATH="+path, "EASYCODE_WRITE_CRASH_BOUNDARY="+boundary)
					if overwrite {
						cmd.Env = append(cmd.Env, "EASYCODE_WRITE_CRASH_OVERWRITE=1")
					}
					cmd.ExtraFiles = []*os.File{writeSignal}
					input, err := cmd.StdinPipe()
					if err != nil {
						t.Fatal(err)
					}
					defer input.Close()
					var output bytes.Buffer
					cmd.Stdout = &output
					cmd.Stderr = &output
					if err := cmd.Start(); err != nil {
						writeSignal.Close()
						t.Fatal(err)
					}
					writeSignal.Close()
					var signal [5]byte
					_, readErr := io.ReadFull(readSignal, signal[:])
					killErr := cmd.Process.Kill()
					waitErr := cmd.Wait()
					if readErr != nil || string(signal[:]) != "ready" || killErr != nil || waitErr == nil {
						t.Fatalf("child handshake/crash: read=%v kill=%v wait=%v %s", readErr, killErr, waitErr, output.String())
					}
					data, readErr := os.ReadFile(target)
					if boundary == "after" {
						if readErr != nil || string(data) != "committed\r\n中文" {
							t.Fatalf("发布后 bytes 丢失 %q %v", data, readErr)
						}
					} else if overwrite {
						if readErr != nil || string(data) != "original" {
							t.Fatalf("发布前覆盖了旧文件 %q %v", data, readErr)
						}
					} else {
						if !os.IsNotExist(readErr) {
							t.Fatal("发布前目标可见", readErr)
						}
						if path != "a" {
							if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
								t.Fatal("发布前目录链可见")
							}
						}
					}
					if boundary == "before" {
						entries, err := os.ReadDir(filepath.Dir(target))
						if !overwrite && path != "a" {
							entries, err = os.ReadDir(root)
						}
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for _, entry := range entries {
							found = found || strings.HasPrefix(entry.Name(), ".easycode-write-")
						}
						if !found {
							t.Fatal("未观察到预期的 SIGKILL 私有暂存残留")
						}
					}
				})
			}
		}
	}
}
