package testsupport

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

// FileConversation 绑定文件输入所需的生产入口和测试请求读取。
type FileConversation interface {
	RequestConversation
	provider.UserInputPreparer
	provider.TranscriptProjector
}

// CheckFileInput 为两家的独立固定原生样本执行同一输入、恢复和投影契约。
func CheckFileInput[C FileConversation](t *testing.T, c C, sampling provider.TurnInput, skill bool, restore func([]provider.NativeCommitEnvelope) (C, error)) C {
	t.Helper()
	u := FileInput(t, skill)
	a, _ := domain.NewInputAssociation("01900000-0000-7000-8000-000000000010", domain.InputInitial, 0, []domain.InputID{u.ID()})
	prepared, err := c.PrepareUserInputs(a, []domain.UserInput{u})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := prepared.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	name := "files"
	if skill {
		name = "skill_files"
	}
	golden, err := os.ReadFile("testdata/" + name + "_input.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(golden), envelope.Payload()) {
		t.Fatalf("native golden differs: %s", envelope.Payload())
	}
	preview, _, err := prepared.Preview()
	if err != nil || preview.History().Turns[0].Messages[0].Files.Len() != 2 || len(c.ProjectHistory().Turns) != 0 {
		t.Fatal("candidate leaked or omitted files", err)
	}
	if err := prepared.Discard(); err != nil {
		t.Fatal(err)
	}
	prepared, err = c.PrepareUserInputs(a, []domain.UserInput{u})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	if prepared.Finalize() == nil {
		t.Fatal("duplicate finalize accepted")
	}
	restored, err := restore([]provider.NativeCommitEnvelope{envelope})
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.TestRequest(sampling)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.TestRequest(sampling)
	if err != nil {
		t.Fatal(err)
	}
	requestGolden, err := os.ReadFile("testdata/" + name + "_request.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(requestGolden), before.Bytes()) {
		t.Fatalf("file request golden differs: %s", before.Bytes())
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) || domain.ContentFingerprint(before.Bytes()) != domain.ContentFingerprint(after.Bytes()) {
		t.Fatal("restore changed actual request")
	}
	for _, fragment := range []string{"# Attached workspace file", "second\\\\nlast", "@not-recursive"} {
		want := 1
		if fragment == "# Attached workspace file" {
			want = 2
		}
		if strings.Count(string(before.Bytes()), fragment) != want {
			t.Fatal("file body duplicated or missing", fragment)
		}
	}
	view := restored.ProjectHistory()
	message := view.Turns[0].Messages[0]
	if message.Text != u.Text() || message.Files.Len() != 2 || message.HasSkill != skill {
		t.Fatal("projection lost independent values")
	}
	message.Files = domain.FileAttachments{}
	view.Turns[0].Messages[0] = message
	transcript, _, err := restored.ProjectTranscriptFrom(0)
	if err != nil || transcript.Items[0].Files.Len() != 2 {
		t.Fatal("transcript missing frozen files", err)
	}
	return restored
}
