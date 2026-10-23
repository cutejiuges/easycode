//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

type unreadSessionInput struct{ t *testing.T }

func (input unreadSessionInput) Read([]byte) (int, error) {
	input.t.Fatal("session listing read stdin")
	return 0, io.EOF
}

func TestSessionsParsingHasNoStorageEffects(t *testing.T) {
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"sessions"}, 2}, {[]string{"sessions", "unknown"}, 2},
		{[]string{"sessions", "--help"}, 0}, {[]string{"sessions", "list", "--help"}, 0},
		{[]string{"sessions", "list", "--limit", "0"}, 2}, {[]string{"sessions", "list", "--limit", "-1"}, 2},
		{[]string{"sessions", "list", "--limit", "201"}, 2}, {[]string{"sessions", "list", "--limit", "many"}, 2},
		{[]string{"sessions", "list", "--limit", "999999999999999999999999"}, 2},
		{[]string{"sessions", "list", "--unknown"}, 2}, {[]string{"sessions", "list", "--json"}, 2},
		{[]string{"sessions", "list", "--print"}, 2}, {[]string{"sessions", "list", "--config", "secret"}, 2},
		{[]string{"sessions", "list", "--resume", "secret"}, 2}, {[]string{"sessions", "list", "extra"}, 2},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			var out, diagnostics bytes.Buffer
			code := run(context.Background(), test.args, unreadSessionInput{t}, &out, &diagnostics, false)
			if code != test.code {
				t.Fatalf("code=%d out=%q err=%q", code, out.String(), diagnostics.String())
			}
			if _, err := os.Stat(filepath.Join(home, ".easycode")); !os.IsNotExist(err) {
				t.Fatalf("created storage: %v", err)
			}
			if strings.Contains(diagnostics.String(), "secret") {
				t.Fatal("raw argument leaked")
			}
		})
	}
}

func TestSessionsListDefaultLimitAndAll(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Chdir(home)
	repository, err := session.OpenOrCreateRepository(filepath.Join(home, ".easycode", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	for i := 0; i < 22; i++ {
		identity, err := session.GenerateRootIdentity()
		if err != nil {
			t.Fatal(err)
		}
		cwd := home
		if i == 21 {
			cwd = filepath.Join(home, "other")
		}
		writer, _, err := session.CreateRootJournal(context.Background(), repository, identity, session.RootJournalConfig{Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "test-model", CreationCWD: cwd})
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		args  []string
		count int
		more  bool
	}{
		{[]string{"sessions", "list"}, 20, true},
		{[]string{"sessions", "list", "--limit", "200"}, 21, false},
		{[]string{"sessions", "list", "--all", "--limit", "200"}, 22, false},
		{[]string{"sessions", "list", "--all", "--limit", "1"}, 1, true},
	} {
		var out, diagnostics bytes.Buffer
		code := run(context.Background(), test.args, unreadSessionInput{t}, &out, &diagnostics, true)
		if code != 0 || strings.Count(out.String(), "  Updated:") != test.count || strings.Contains(out.String(), "More indexed sessions") != test.more || diagnostics.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%q err=%q", test.args, code, out.String(), diagnostics.String())
		}
	}
}

func TestSessionsNameRemainsAChatPrompt(t *testing.T) {
	clearCLIEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "sessions list") {
			t.Error("prompt missing")
		}
		http.Error(w, "fixture unavailable", http.StatusBadRequest)
	}))
	defer server.Close()
	var out, diagnostics bytes.Buffer
	code := run(context.Background(), []string{"--config", writeCLIConfig(t, server.URL), "--print", "sessions list"}, unreadSessionInput{t}, &out, &diagnostics, true)
	if code != 1 || calls.Load() != 1 || strings.Contains(out.String(), "Sessions in") {
		t.Fatalf("code=%d calls=%d out=%q err=%q", code, calls.Load(), out.String(), diagnostics.String())
	}
}

type brokenSessionsWriter struct{}

func (brokenSessionsWriter) Write([]byte) (int, error) { return 0, errors.New("broken output") }
func TestSessionsHelpAndUsageOutputFailure(t *testing.T) {
	if runSessions(context.Background(), []string{"--help"}, brokenSessionsWriter{}, io.Discard) != 1 {
		t.Fatal("help ignored output error")
	}
	if runSessions(context.Background(), nil, io.Discard, brokenSessionsWriter{}) != 1 {
		t.Fatal("usage ignored output error")
	}
}
