package session

import (
	"bytes"
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestCurrentQuestionFixturesAndCorruption(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, state := range []string{"ready", "started", "result", "complete", "skip", "unavailable", "forged"} {
			t.Run(string(family)+"/"+state, func(t *testing.T) {
				data := readCurrentFixture(t, "question_"+string(family)+"_"+state+".jsonl")
				for _, forbidden := range [][]byte{[]byte("/Users/"), []byte("question_request_id"), []byte("schema_version"), []byte("api_key"), []byte("Authorization")} {
					if bytes.Contains(data, forbidden) {
						t.Fatal("fixture contains forbidden copy", string(forbidden))
					}
				}
				repo, lease := installCurrentFixture(t, data)
				defer repo.Close()
				defer lease.Close()
				loaded, err := NewLoader().Load(context.Background(), lease)
				if err != nil {
					t.Fatal(err)
				}
				// 每条输入均来自仓库固定 bytes，encoder 只验证输出一致性。
				lines := bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'})
				for i, r := range loaded.Records {
					_, b, err := EncodeRecord(r)
					if err != nil || !bytes.Equal(b, lines[i]) {
						t.Fatal("current record bytes changed", err)
					}
				}
				plan, err := NewReplayPlanner().Plan(loaded)
				if state == "forged" {
					if err == nil {
						t.Fatal("forged selected label accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if plan.SessionMetadata.Provider != family || len(plan.Tools) < 2 || plan.Tools[0].Invocation.Capability() != tool.CapabilityRead || plan.Tools[1].Invocation.Capability() != tool.CapabilityQuestion {
					t.Fatal("mixed order lost")
				}
				q := plan.Tools[1]
				if state == "ready" || state == "started" {
					if q.HasResult {
						t.Fatal("invented answer")
					}
					calls := plan.ToolRecovery.Calls()
					want := ReplayedToolCallReady
					if state == "started" {
						want = ReplayedToolCallStarted
					}
					if calls[len(calls)-1].State != want {
						t.Fatal("wrong crash boundary")
					}
					return
				}
				if !q.HasResult {
					t.Fatal("missing durable answer")
				}
				if state == "unavailable" {
					if q.Result.Code() != "user_input_unavailable" {
						t.Fatal("headless result changed")
					}
					return
				}
				a, err := q.Result.QuestionAnswers()
				if err != nil {
					t.Fatal(err)
				}
				if state == "skip" {
					if !a.AllSkipped() {
						t.Fatal("skipped question invented choice")
					}
				} else if a.Answers()[0].Text != " 保留说明\n" || a.Answers()[0].SelectedOptions[0] != 0 {
					t.Fatal("frozen answer changed")
				}
				if state == "complete" && plan.ToolRecovery != nil {
					t.Fatal("completed question requires recovery")
				}
			})
		}
	}
}
