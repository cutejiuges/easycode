package anthropic

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// fileStopBridgeHistory 覆盖两次反馈、独立多文件纠正及超过二十块的采样窗口。
func fileStopBridgeHistory(t *testing.T) (*Conversation, []provider.RestoreRecord) {
	t.Helper()
	c, _, _ := stopTestConversation(t)
	frozen := frozenFileInput(t, true)
	for index := uint32(0); index < 2; index++ {
		feedback, err := stopFeedbackEntry(stopTestFeedback(t, index))
		if err != nil {
			t.Fatal(err)
		}
		c.history.commit(feedback)
		user, err := domain.NewUserInput(domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%012d", 12+index)), frozen.Text())
		if err != nil {
			t.Fatal(err)
		}
		skill, _ := frozen.SkillAttachment()
		user, err = user.WithSkillAttachment(skill)
		if err != nil {
			t.Fatal(err)
		}
		user, err = user.WithFileAttachments(frozen.FileAttachments())
		if err != nil {
			t.Fatal(err)
		}
		a, err := domain.NewInputAssociation(stopTestTurn, domain.InputSteering, index+1, []domain.InputID{user.ID()})
		if err != nil {
			t.Fatal(err)
		}
		entry, err := userInputEntry(a, []domain.UserInput{user})
		if err != nil {
			t.Fatal(err)
		}
		c.history.commit(entry)
		if index == 0 {
			sample := stopTestSample(stopTestTurn, "long answer")
			for i := 0; i < 18; i++ {
				sample.Assistant.Content = append(sample.Assistant.Content, NativeItem{Type: blockTypeText, Text: fmt.Sprintf("part-%02d", i)})
			}
			c.history.commit(sample)
		}
	}
	var envelopes []provider.NativeCommitEnvelope
	for _, entry := range c.history.snapshot() {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	return c, testRestoreRecords(t, envelopes)
}

func TestFileStopCacheBridgeUsesActualMessagesAndRestoresExactly(t *testing.T) {
	c, records := fileStopBridgeHistory(t)
	value, err := testRestoreAfterPreview(t, c.provider, c.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := value.(*Conversation)
	beforeNative, err := codec.MarshalStable(c.history.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/files_stop_bridge_markers.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		WithoutProject []string `json:"without_project"`
		WithProject    []string `json:"with_project"`
	}
	if err := codec.UnmarshalStrict(golden, &expected); err != nil {
		t.Fatal(err)
	}
	for _, project := range []bool{false, true} {
		input, err := testSamplingInput(t, c.provider.config.Inference).WithToolCatalog(testAnthropicToolCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if project {
			input, err = input.WithProjectInstructions(testAnthropicProjectInstructions(t, "AGENTS.md", "Project instructions."))
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, summary := range []bool{false, true} {
			for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
				t.Run(fmt.Sprintf("project=%t/summary=%t/%s", project, summary, mode), func(t *testing.T) {
					var original []byte
					for _, conversation := range []*Conversation{c, restored} {
						plan, err := conversation.planSamplingRequest(conversation.history.snapshot(), input)
						if err != nil {
							t.Fatal(err)
						}
						if summary {
							plan.request.Messages = append(plan.request.Messages, newUserMessage("one-off summary guidance"))
							plan.purpose = domain.RequestSummary
						}
						body, err := plan.body(mode)
						if err != nil {
							t.Fatal(err)
						}
						if original == nil {
							original = body.Bytes()
						} else if !bytes.Equal(original, body.Bytes()) {
							t.Fatal("restored cache request differs")
						}
						if bytes.Count(body.Bytes(), []byte("# Attached workspace file")) != 4 || !bytes.Contains(body.Bytes(), []byte("PRIVATE-OPAQUE")) {
							t.Fatal("file or opaque content changed")
						}
						want := expected.WithoutProject
						if project {
							want = expected.WithProject
						}
						if mode == domain.AnthropicCacheDisabled {
							want = nil
						}
						if got := fileBridgeMarkers(t, body.Bytes(), mode); !slices.Equal(got, want) {
							t.Fatalf("cache bridge moved away from the last pre-sample file block: got=%v want=%v", got, want)
						}
					}
				})
			}
		}
	}
	afterNative, _ := codec.MarshalStable(restored.history.snapshot())
	unchangedNative, _ := codec.MarshalStable(c.history.snapshot())
	left, leftErr := c.HistoryFootprint()
	right, rightErr := restored.HistoryFootprint()
	if !bytes.Equal(beforeNative, afterNative) || !bytes.Equal(beforeNative, unchangedNative) || leftErr != nil || rightErr != nil || left != right || !reflect.DeepEqual(c.ProjectHistory(), restored.ProjectHistory()) || reasoningCacheSources(t, c) != reasoningCacheSources(t, restored) {
		t.Fatal("cache overlay changed native content, projection, footprint or fingerprints")
	}
}

func fileBridgeMarkers(t *testing.T, data []byte, mode domain.AnthropicCacheMode) []string {
	t.Helper()
	type block struct {
		Cache *cacheControl `json:"cache_control"`
	}
	var request struct {
		Tools    []block `json:"tools"`
		Messages []struct {
			Content []block `json:"content"`
		} `json:"messages"`
	}
	if err := codec.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	var markers []string
	appendMarker := func(location string, marker *cacheControl) {
		if marker == nil {
			return
		}
		ttl := ""
		if mode == domain.AnthropicCache1h {
			ttl = "1h"
		}
		if marker.Type != "ephemeral" || marker.TTL != ttl {
			t.Fatalf("incorrect cache TTL at %s: %+v", location, marker)
		}
		markers = append(markers, location)
	}
	for i, tool := range request.Tools {
		appendMarker(fmt.Sprintf("tool/%d", i), tool.Cache)
	}
	for i, message := range request.Messages {
		for j, item := range message.Content {
			appendMarker(fmt.Sprintf("message/%d/block/%d", i, j), item.Cache)
		}
	}
	return markers
}
