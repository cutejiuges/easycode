//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func writeCall(t *testing.T, path, content string) tool.WriteInvocation {
	t.Helper()
	input, err := tool.NewWriteInput(path, content)
	if err != nil {
		t.Fatal(err)
	}
	call, err := tool.NewWriteReadyCall("write-call", input)
	if err != nil {
		t.Fatal(err)
	}
	id, err := tool.GenerateInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	v, err := tool.NewWriteInvocation(id, call)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func writeComponents(t *testing.T, root string) (*Workspace, *WritePreparer, *WriteExecutor, *ReadExecutor) {
	t.Helper()
	w, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	p, err := NewWritePreparer(w)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewWriteExecutor(w)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReadExecutor(w)
	if err != nil {
		t.Fatal(err)
	}
	return w, p, e, r
}

func readObservation(t *testing.T, r *ReadExecutor, path string) tool.ReadObservation {
	t.Helper()
	e := r.Execute(context.Background(), invocationFor(t, path, 1, 2000))
	if e.Observation().Kind() != tool.ReadObservationComplete {
		t.Fatal("缺少完整读取", e.Result().Code())
	}
	return e.Observation()
}

func assertNoStaging(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".easycode-write-") {
			t.Errorf("残留暂存项 %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteCreateExactBytesAndPrivateDirectoryTree(t *testing.T) {
	for _, path := range []string{"a", "src/new/pkg/a"} {
		for _, content := range []string{"", "\ufeff中文\r\n尾行", "line\n"} {
			t.Run(path+content, func(t *testing.T) {
				root := t.TempDir()
				if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
					t.Fatal(err)
				}
				_, p, e, _ := writeComponents(t, root)
				c, err := p.Prepare(context.Background(), writeCall(t, path, content), tool.UnavailableReadObservation())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(root, path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("prepare 创建了目标", err)
				}
				assertNoStaging(t, root)
				e.hooks.beforePublish = func() {
					target := path
					if strings.HasPrefix(path, "src/") {
						target = "src/new"
					}
					if _, err := os.Stat(filepath.Join(root, target)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("发布前目标已存在", err)
					}
				}
				result := e.Execute(context.Background(), c)
				if result.Status() != tool.ResultSuccess {
					t.Fatalf("写入失败 %s/%s", result.Status(), result.Code())
				}
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(data) != content {
					t.Fatalf("bytes 不等价 %q %v", data, err)
				}
				info, err := os.Stat(filepath.Join(root, path))
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal("新文件权限错误", err)
				}
				for _, dir := range c.Directories() {
					info, err := os.Stat(filepath.Join(root, dir))
					if err != nil || info.Mode().Perm() != 0o700 {
						t.Fatal("新目录权限错误", err)
					}
				}
				assertNoStaging(t, root)
			})
		}
	}
}

func TestWriteRequiresFullReadAndRevalidatesBase(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a")
	if err := os.WriteFile(path, []byte("old\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, p, e, r := writeComponents(t, root)
	v := writeCall(t, "a", "new\r\n")
	if _, err := p.Prepare(context.Background(), v, tool.UnavailableReadObservation()); err == nil || err.(*WritePreparationError).Code() != "write_requires_full_read" {
		t.Fatal("未拒绝缺失证据", err)
	}
	observation := readObservation(t, r, "a")
	if _, err := p.Prepare(context.Background(), writeCall(t, "a", "old\r\n"), observation); err == nil || err.(*WritePreparationError).Code() != "write_no_changes" {
		t.Fatal("no-op 未拒绝", err)
	}
	c, err := p.Prepare(context.Background(), v, observation)
	if err != nil {
		t.Fatal(err)
	}
	if c.Operation() != tool.WriteOverwrite || c.Mode() != 0o755 {
		t.Fatal("覆盖候选错误")
	}
	result := e.Execute(context.Background(), c)
	if result.Status() != tool.ResultSuccess {
		t.Fatal(result.Code())
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("可执行位未保留", err)
	}
	if _, err := p.Prepare(context.Background(), v, observation); err == nil || err.(*WritePreparationError).Code() != "write_conflict" {
		t.Fatal("旧证据未失效", err)
	}
	assertNoStaging(t, root)
}

func TestWriteCreationDoesNotReplaceCompetitors(t *testing.T) {
	for _, tree := range []bool{false, true} {
		for _, kind := range []string{"file", "empty-file", "directory", "symlink"} {
			t.Run(kind+map[bool]string{false: "-file", true: "-tree"}[tree], func(t *testing.T) {
				root := t.TempDir()
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, p, e, _ := writeComponents(t, root)
				path := "a"
				if tree {
					path = "a/b/file"
				}
				c, err := p.Prepare(context.Background(), writeCall(t, path, "owned"), tool.UnavailableReadObservation())
				if err != nil {
					t.Fatal(err)
				}
				// 在最终复验之后、系统调用之前制造竞争，实际验证 no-replace 原语。
				e.hooks.publish = func(fd int, from, to string, noReplace bool) error {
					if !noReplace {
						t.Fatal("创建降级为普通 rename")
					}
					var err error
					switch kind {
					case "file":
						err = os.WriteFile(filepath.Join(root, "a"), []byte("competitor"), 0o600)
					case "empty-file":
						err = os.WriteFile(filepath.Join(root, "a"), nil, 0o600)
					case "directory":
						err = os.Mkdir(filepath.Join(root, "a"), 0o700)
					case "symlink":
						err = os.Symlink(outside, filepath.Join(root, "a"))
					}
					if err != nil {
						t.Fatal(err)
					}
					return publishWriteEntry(fd, from, to, noReplace)
				}
				result := e.Execute(context.Background(), c)
				if result.Status() != tool.ResultConflict {
					t.Fatalf("未报告冲突 %s/%s", result.Status(), result.Code())
				}
				if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
					t.Fatal("修改了外部目标", err)
				}
				if kind == "file" {
					if data, _ := os.ReadFile(filepath.Join(root, "a")); string(data) != "competitor" {
						t.Fatal("覆盖了竞争者")
					}
				}
				assertNoStaging(t, root)
			})
		}
	}
}

func TestWriteFailureAndCancellationPublicationBoundaries(t *testing.T) {
	for _, stage := range []string{"entry-cancel", "write", "short-write", "chmod", "file-sync", "before-cancel", "after-cancel", "parent-sync", "unsupported", "ambiguous-publish", "close", "unlink"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			_, p, e, _ := writeComponents(t, root)
			c, err := p.Prepare(context.Background(), writeCall(t, "a", "new"), tool.UnavailableReadObservation())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := tool.ResultError
			published := false
			orphan := false
			switch stage {
			case "entry-cancel":
				cancel()
				want = tool.ResultCancelled
			case "write":
				e.hooks.write = func(*os.File, []byte) error { return errors.New("injected") }
			case "short-write":
				e.hooks.write = func(f *os.File, _ []byte) error { _, err := f.Write([]byte("n")); return err }
			case "chmod":
				e.hooks.chmod = func(*os.File, os.FileMode) error { return errors.New("injected") }
			case "file-sync":
				e.hooks.fileSync = func(*os.File) error { return errors.New("injected") }
			case "before-cancel":
				e.hooks.beforePublish = cancel
				want = tool.ResultCancelled
			case "after-cancel":
				e.hooks.afterPublish = cancel
				want = tool.ResultSuccess
				published = true
			case "parent-sync":
				e.hooks.directorySync = func(*os.File) error { return errors.New("injected") }
				want = tool.ResultOutcomeUncertain
				published = true
			case "unsupported":
				e.hooks.publish = func(int, string, string, bool) error { return unix.ENOSYS }
			case "ambiguous-publish":
				e.hooks.publish = func(fd int, from, to string, noReplace bool) error {
					if err := publishWriteEntry(fd, from, to, noReplace); err != nil {
						t.Fatal(err)
					}
					return unix.EIO
				}
				want = tool.ResultOutcomeUncertain
				published = true
			case "close":
				e.hooks.close = func(*os.File) error { return errors.New("injected") }
				want = tool.ResultOutcomeUncertain
				published = true
			case "unlink":
				e.hooks.fileSync = func(*os.File) error { return errors.New("injected") }
				e.hooks.unlink = func(int, string, bool) error { return unix.EIO }
				want = tool.ResultOutcomeUncertain
				orphan = true
			}
			result := e.Execute(ctx, c)
			if result.Status() != want {
				t.Fatalf("结果 %s/%s，期望 %s", result.Status(), result.Code(), want)
			}
			data, err := os.ReadFile(filepath.Join(root, "a"))
			if published {
				if err != nil || string(data) != "new" {
					t.Fatal("已发布目标错误", err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal("发布前失败修改目标", err)
			}
			if !orphan {
				assertNoStaging(t, root)
			}
		})
	}
}

func TestWriteOverwriteConflictsPreserveExternalContent(t *testing.T) {
	for _, change := range []string{"content", "inode", "mode", "delete", "leaf-symlink", "ancestor-symlink"} {
		for _, beforePublish := range []bool{false, true} {
			t.Run(change+map[bool]string{false: "-reopen", true: "-publish"}[beforePublish], func(t *testing.T) {
				root := t.TempDir()
				if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "dir/a")
				if err := os.WriteFile(path, []byte("base"), 0o600); err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				outsideFile := filepath.Join(outside, "a")
				if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, p, e, r := writeComponents(t, root)
				c, err := p.Prepare(context.Background(), writeCall(t, "dir/a", "new"), readObservation(t, r, "dir/a"))
				if err != nil {
					t.Fatal(err)
				}
				mutate := func() {
					var err error
					switch change {
					case "content":
						err = os.WriteFile(path, []byte("changed"), 0o600)
					case "inode":
						err = os.Rename(path, path+".old")
						if err == nil {
							err = os.WriteFile(path, []byte("base"), 0o600)
						}
					case "mode":
						err = os.Chmod(path, 0o644)
					case "delete":
						err = os.Remove(path)
					case "leaf-symlink":
						err = os.Remove(path)
						if err == nil {
							err = os.Symlink(outsideFile, path)
						}
					case "ancestor-symlink":
						err = os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "safe"))
						if err == nil {
							err = os.Symlink(outside, filepath.Join(root, "dir"))
						}
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if beforePublish {
					e.hooks.beforePublish = mutate
				} else {
					mutate()
				}
				result := e.Execute(context.Background(), c)
				if result.Status() != tool.ResultConflict {
					t.Fatalf("未拒绝变化 %s/%s", result.Status(), result.Code())
				}
				if data, _ := os.ReadFile(outsideFile); string(data) != "outside" {
					t.Fatal("外部目标被修改")
				}
				if change == "content" {
					if data, _ := os.ReadFile(path); string(data) != "changed" {
						t.Fatal("覆盖了修改")
					}
				}
				assertNoStaging(t, root)
			})
		}
	}
}
