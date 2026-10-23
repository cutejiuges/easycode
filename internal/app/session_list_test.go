//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/session"
	"easycode/internal/session/catalog"
)

func TestListSessionsWithoutChatConfiguration(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "invalid"}[invalid], func(t *testing.T) {
			home := privateAppTempDir(t)
			t.Setenv("HOME", home)
			t.Chdir(home)
			if invalid {
				path := filepath.Join(home, ".config", "easycode")
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "config.json"), []byte("not-json-secret"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// 若错误地进入聊天指令发现，此目录型 AGENTS.md 必须导致失败。
			if err := os.Mkdir(filepath.Join(home, "AGENTS.md"), 0700); err != nil {
				t.Fatal(err)
			}
			var out, diagnostics bytes.Buffer
			got := ListSessions(context.Background(), SessionListOptions{Limit: 20, Output: &out, ErrorOutput: &diagnostics})
			if got.ExitCode() != 0 || !strings.Contains(out.String(), "No indexed sessions") || diagnostics.Len() != 0 {
				t.Fatalf("%+v %q %q", got, out.String(), diagnostics.String())
			}
			if countJournalFiles(t, filepath.Join(home, ".easycode", "sessions")) != 0 {
				t.Fatal("listing created a journal")
			}
		})
	}
}

type listCatalogStub struct {
	steps    *[]string
	fail     string
	cancelAt string
	cancel   context.CancelFunc
	report   catalog.ReconciliationReport
}

func (s *listCatalogStub) step(name string) error {
	*s.steps = append(*s.steps, name)
	if s.cancelAt == name {
		s.cancel()
	}
	if s.fail == name {
		return errors.New("private storage cause")
	}
	return nil
}
func (s *listCatalogStub) Open(context.Context) error { return s.step("open") }
func (s *listCatalogStub) Reconcile(context.Context, *session.Repository) (catalog.ReconciliationReport, error) {
	return s.report, s.step("reconcile")
}
func (s *listCatalogStub) List(context.Context, catalog.ListQuery) (catalog.ListResult, error) {
	return catalog.ListResult{}, s.step("list")
}
func (s *listCatalogStub) Close() error { return s.step("close") }

type listWriterFunc func([]byte) (int, error)

func (f listWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestListSessionsLifecycleFailuresAndCancellation(t *testing.T) {
	cases := []struct {
		fail, cancelAt string
		want           []string
	}{
		{fail: "repository", want: []string{"repository"}},
		{fail: "construct", want: []string{"repository", "construct", "repository-close"}},
		{fail: "open", want: []string{"repository", "construct", "open", "close", "repository-close"}},
		{fail: "reconcile", want: []string{"repository", "construct", "open", "reconcile", "close", "repository-close"}},
		{fail: "list", want: []string{"repository", "construct", "open", "reconcile", "list", "close", "repository-close"}},
		{fail: "close", want: []string{"repository", "construct", "open", "reconcile", "list", "close", "repository-close"}},
		{fail: "repository-close", want: []string{"repository", "construct", "open", "reconcile", "list", "close", "repository-close"}},
		{cancelAt: "repository", want: []string{"repository", "repository-close"}},
		{cancelAt: "construct", want: []string{"repository", "construct", "close", "repository-close"}},
		{cancelAt: "open", want: []string{"repository", "construct", "open", "close", "repository-close"}},
		{cancelAt: "reconcile", want: []string{"repository", "construct", "open", "reconcile", "close", "repository-close"}},
		{cancelAt: "list", want: []string{"repository", "construct", "open", "reconcile", "list", "close", "repository-close"}},
		{cancelAt: "close", want: []string{"repository", "construct", "open", "reconcile", "list", "close", "repository-close"}},
	}
	for _, test := range cases {
		t.Run(test.fail+"/"+test.cancelAt, func(t *testing.T) {
			var steps []string
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stub := &listCatalogStub{steps: &steps, fail: test.fail, cancelAt: test.cancelAt, cancel: cancel}
			deps := sessionListDependencies{
				workingDirectory: func() (string, error) { t.Fatal("all queried cwd"); return "", nil },
				openRepository:   func(string) (*session.Repository, error) { return nil, stub.step("repository") },
				closeRepository:  func(*session.Repository) error { return stub.step("repository-close") },
				newCatalog:       func(catalog.Config) (sessionListCatalog, error) { return stub, stub.step("construct") },
			}
			var out, diagnostics bytes.Buffer
			got := listSessions(ctx, SessionListOptions{All: true, Limit: 20, SessionDataRoot: "/sessions", SessionCatalogPath: "/state.sqlite", Output: &out, ErrorOutput: &diagnostics}, deps)
			if got.ExitCode() != 1 || out.Len() != 0 || !reflect.DeepEqual(steps, test.want) || strings.Contains(got.Failure.Message, "private") {
				t.Fatalf("%+v steps=%v want=%v out=%q", got, steps, test.want, out.String())
			}
		})
	}
}

func TestListSessionsClosesBeforeOutputAndReportsGlobalWarnings(t *testing.T) {
	for _, failure := range []string{"none", "stdout", "stderr", "short", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			var steps []string
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stub := &listCatalogStub{steps: &steps, report: catalog.ReconciliationReport{Busy: 2, Invalid: 3, BusyUnindexed: true}}
			deps := sessionListDependencies{
				openRepository:  func(string) (*session.Repository, error) { return nil, stub.step("repository") },
				closeRepository: func(*session.Repository) error { return stub.step("repository-close") },
				newCatalog:      func(catalog.Config) (sessionListCatalog, error) { return stub, nil },
			}
			var out, diagnostics bytes.Buffer
			writer := func(name string, buffer *bytes.Buffer) io.Writer {
				return listWriterFunc(func(p []byte) (int, error) {
					if steps[len(steps)-1] != "repository-close" {
						t.Fatal("output holds storage")
					}
					if failure == name {
						return 0, errors.New("write failed")
					}
					if name == "stdout" && failure == "short" {
						return 0, nil
					}
					if name == "stderr" && failure == "cancel" {
						cancel()
					}
					return buffer.Write(p)
				})
			}
			got := listSessions(ctx, SessionListOptions{All: true, Limit: 20, SessionDataRoot: "/sessions", SessionCatalogPath: "/state.sqlite", Output: writer("stdout", &out), ErrorOutput: writer("stderr", &diagnostics)}, deps)
			want := 1
			if failure == "none" {
				want = 0
			}
			if got.ExitCode() != want {
				t.Fatalf("%+v", got)
			}
			if failure == "none" && (!strings.Contains(diagnostics.String(), "storage-wide scan found 2 busy and 3 invalid") || !strings.Contains(out.String(), "No indexed sessions")) {
				t.Fatalf("%q %q", out.String(), diagnostics.String())
			}
			if failure != "none" && out.Len() != 0 {
				t.Fatal("failure printed success")
			}
		})
	}
}

func TestListSessionsRejectsBeforeIO(t *testing.T) {
	for _, limit := range []int{0, -1, 201} {
		got := listSessions(context.Background(), SessionListOptions{Limit: limit, Output: io.Discard, ErrorOutput: io.Discard}, sessionListDependencies{})
		if got.ExitCode() != 2 {
			t.Fatalf("%+v", got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := listSessions(ctx, SessionListOptions{Limit: 20, Output: io.Discard, ErrorOutput: io.Discard}, sessionListDependencies{})
	if got.ExitCode() != 1 {
		t.Fatalf("%+v", got)
	}
}
