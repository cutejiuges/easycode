package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestCompactionFixedFixturesResumeAndRepair(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []string{"completed", "started", "half_completion", "source", "native_canary", "session_canary", "summary"} {
			t.Run(string(family)+"/"+mode, func(t *testing.T) {
				original, err := os.ReadFile(filepath.Join("..", "session", "testdata", "current", "compaction_completed_"+string(family)+".jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSuffix(original, []byte{'\n'}), []byte{'\n'})
				// 固定事实源必须能逐字节往返，测试永不生成或更新 fixture。
				for _, line := range lines {
					record, err := session.DecodeRecord(line)
					if err != nil {
						t.Fatal(err)
					}
					_, encoded, err := session.EncodeRecord(record)
					if err != nil || !bytes.Equal(encoded, line) {
						t.Fatal("fixture canonical bytes changed")
					}
				}
				data := append([]byte(nil), original...)
				valid := mode == "completed" || mode == "started" || mode == "half_completion"
				switch mode {
				case "started", "half_completion":
					data = bytes.Join(lines[:len(lines)-1], []byte{'\n'})
					data = append(data, '\n')
					if mode == "half_completion" {
						data = append(data, lines[len(lines)-1][:25]...)
					}
				case "source":
					for i, line := range lines {
						if bytes.Contains(line, []byte(`"event_kind":"compaction_started"`)) {
							// 保持 checksum 正确，仅破坏语义来源。
							start := bytes.Index(line, []byte(`"digest":"`)) + len(`"digest":"`)
							if start < len(`"digest":"`) {
								t.Fatal("missing digest")
							}
							line = append([]byte(nil), line...)
							copy(line[start:start+64], strings.Repeat("f", 64))
							lines[i] = resealCompactionFixture(line)
						}
					}
					data = append(bytes.Join(lines, []byte{'\n'}), '\n')
				case "native_canary":
					for i, line := range lines {
						if bytes.Contains(line, []byte(`"event_kind":"provider_native_commit"`)) {
							lines[i] = resealCompactionFixture(bytes.Replace(line, []byte(`"payload_version":3`), []byte(`"payload_version":2`), 1))
						}
					}
					data = append(bytes.Join(lines, []byte{'\n'}), '\n')
				case "session_canary":
					for i, line := range lines {
						line = bytes.Replace(line, []byte(`"schema_version":7`), []byte(`"schema_version":6`), 1)
						line = bytes.Replace(line, []byte(`"payload_version":7`), []byte(`"payload_version":6`), 1)
						lines[i] = resealCompactionFixture(line)
					}
					data = append(bytes.Join(lines, []byte{'\n'}), '\n')
				case "summary":
					last := lines[len(lines)-1]
					last = bytes.Replace(last, []byte("answer-2"), []byte("forged-summary"), 1)
					lines[len(lines)-1] = resealCompactionFixture(last)
					data = append(bytes.Join(lines, []byte{'\n'}), '\n')
				}
				if !valid {
					data = append(data, []byte(`{"torn":`)...)
				}
				thread := domain.ThreadID("019a39ab-0000-7000-8000-000000000002")
				service := newTestSessionService(t)
				defer service.close()
				lease, err := service.repository.Create(context.Background(), thread)
				if err != nil {
					t.Fatal(err)
				}
				if err := lease.Close(); err != nil {
					t.Fatal(err)
				}
				path, _ := service.repository.JournalPath(thread)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				resource, err := newProviderResource(config.Config{Provider: config.Provider{Family: family, Model: "claude-test", BaseURL: "https://example.invalid", APIKey: secret.New("fixture-private-key")}})
				if err != nil {
					t.Fatal(err)
				}
				defer resource.Close()
				wire := "responses"
				if family == domain.ProviderAnthropic {
					wire = "messages"
				}
				resumed, err := service.resume(context.Background(), resource, wire, "claude-test", thread)
				if (err == nil) != valid {
					t.Fatalf("valid=%t err=%v", valid, err)
				}
				if valid {
					view, err := resumed.conversation.ProjectActiveContext()
					if err != nil {
						t.Fatal(err)
					}
					_, summary := view.Summary()
					if summary != (mode == "completed") {
						t.Fatal("wrong recovered window")
					}
					if len(resumed.conversation.ProjectHistory().Turns) != 1 {
						t.Fatal("full history lost")
					}
					if err := resumed.writer.Close(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !valid && !bytes.Equal(after, data) {
					t.Fatal("semantic rejection repaired journal")
				}
				if mode == "completed" && !bytes.Equal(after, original) {
					t.Fatal("completed restore appended facts")
				}
				if mode == "started" || mode == "half_completion" {
					prefix := append(bytes.Join(lines[:len(lines)-1], []byte{'\n'}), '\n')
					if !bytes.HasPrefix(after, prefix) || bytes.Count(after, []byte(`"event_kind":"compaction_failed"`)) != 1 || !bytes.Contains(after, []byte(`"code":"compaction_interrupted"`)) {
						t.Fatal("interrupted operation not reconciled locally")
					}
				}
			})
		}
	}
}

// resealCompactionFixture 只为损坏注入重算 checksum，不生成事实源 fixture。
func resealCompactionFixture(line []byte) []byte {
	prefix := line[:bytes.LastIndex(line, []byte(`,"checksum":`))]
	sum := sha256.Sum256(append(append([]byte(nil), prefix...), '}'))
	result := append(append([]byte(nil), prefix...), []byte(`,"checksum":"`)...)
	result = append(result, []byte(hex.EncodeToString(sum[:]))...)
	return append(result, []byte(`"}`)...)
}
