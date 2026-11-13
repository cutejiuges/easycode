package main

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type pluginCLIOutput struct{ bytes.Buffer }

func (*pluginCLIOutput) Close() error { return nil }

func TestCLIPluginPackagesBothProvidersAndHeadlessModes(t *testing.T) {
	fixtures, err := filepath.Abs("../../internal/extension/plugin/testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"easycode", "claude", "codex", "agentplugins"} {
		for _, family := range []string{"openai", "anthropic"} {
			for _, mode := range []string{"print", "json", "stream"} {
				t.Run(kind+"/"+family+"/"+mode, func(t *testing.T) {
					home, err := filepath.EvalSymlinks(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					t.Setenv("HOME", home)
					workspace := t.TempDir()
					t.Chdir(workspace)
					pack := t.TempDir()
					if err := os.CopyFS(pack, os.DirFS(filepath.Join(fixtures, kind))); err != nil {
						t.Fatal(err)
					}
					name := map[string]string{"easycode": "easyqa", "claude": "claudeqa", "codex": "codexqa", "agentplugins": "agentqa"}[kind] + ":review"
					var mu sync.Mutex
					var requests [][]byte
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						mu.Lock()
						requests = append(requests, body)
						n := len(requests)
						mu.Unlock()
						w.Header().Set("Content-Type", "text/event-stream")
						event := func(value string) { _, _ = io.WriteString(w, "data: "+value+"\n\n") }
						if family == "openai" {
							event(fmt.Sprintf(`{"type":"response.created","response":{"id":"cli-%d"}}`, n))
							event(`{"type":"response.output_text.delta","delta":"Plugin CLI complete"}`)
							event(fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"message","id":"message-%d","role":"assistant","content":[{"type":"output_text","text":"Plugin CLI complete"}]}}`, n))
							event(fmt.Sprintf(`{"type":"response.completed","response":{"id":"cli-%d"}}`, n))
						} else {
							event(fmt.Sprintf(`{"type":"message_start","message":{"id":"cli-%d","model":"fixture-model"}}`, n))
							event(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
							event(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Plugin CLI complete"}}`)
							event(`{"type":"content_block_stop","index":0}`)
							event(`{"type":"message_stop"}`)
						}
					}))
					defer server.Close()
					configuration := filepath.Join(t.TempDir(), "config.json")
					configBytes, _ := codec.MarshalStable(map[string]string{"provider": family, "base_url": server.URL, "api_key": "plugin-cli-fixture", "model": "fixture-model"})
					if err := os.WriteFile(configuration, configBytes, 0600); err != nil {
						t.Fatal(err)
					}
					args := []string{"--config", configuration, "--plugin-dir", pack, "--plugin-dir", filepath.Join(t.TempDir(), "missing")}
					input := io.ReadCloser(io.NopCloser(strings.NewReader("")))
					if mode == "stream" {
						args = append(args, "--json", "--input-format", "stream-json")
						input = io.NopCloser(strings.NewReader(`{"version":1,"type":"input.submit","request_id":"plugin","text":"$` + name + ` exact args"}` + "\n"))
					} else {
						args = append(args, "--"+mode, "$"+name+" exact args")
					}
					output := &pluginCLIOutput{}
					var stderr bytes.Buffer
					if code := run(context.Background(), args, input, output, &stderr, false); code != 0 || !strings.Contains(output.String(), "Plugin CLI complete") || stderr.Len() != 0 {
						t.Fatalf("CLI: exit=%d err=%q out=%q", code, stderr.String(), output.String())
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if len(captured) != 1 || !bytes.Contains(captured[0], []byte("Review exact args")) || bytes.Contains(captured[0], []byte(pack)) {
						t.Fatal("CLI flag not consumed by actual provider request")
					}
					if mode != "print" {
						for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
							if !codec.Valid(line) || bytes.Contains(line, []byte("plugin_manifest_")) {
								t.Fatal("headless protocol polluted", string(line))
							}
						}
					}
					if mode != "print" {
						return
					}
					var thread string
					if err := filepath.WalkDir(filepath.Join(home, ".easycode", "sessions"), func(path string, d os.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
							thread = strings.TrimSuffix(filepath.Base(path), ".jsonl")
						}
						return nil
					}); err != nil || thread == "" {
						t.Fatal("missing CLI session", err)
					}
					for _, selector := range [][]string{{"--continue"}, {"--resume", thread}} {
						output.Reset()
						stderr.Reset()
						args := []string{"--config", configuration, "--plugin-dir", pack, "--print"}
						args = append(args, selector...)
						args = append(args, "$"+name+" resumed args")
						if code := run(context.Background(), args, strings.NewReader(""), output, &stderr, false); code != 0 {
							t.Fatalf("resume CLI %v: %d %s", selector, code, stderr.String())
						}
					}
					mu.Lock()
					count := len(requests)
					mu.Unlock()
					if count != 3 {
						t.Fatal("resume flags lost plugin inputs", count)
					}
				})
			}
		}
	}
}
