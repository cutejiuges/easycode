package anthropic

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func frozenFileInput(t *testing.T, skill bool) domain.UserInput {
	t.Helper()
	selection, _ := domain.NewLineFileSelection(2, 8)
	ref, _ := domain.NewFileReference("src/a.go", selection)
	a, err := domain.FreezeFileAttachment(ref, "first\r\nsecond\nlast")
	if err != nil {
		t.Fatal(err)
	}
	ref, _ = domain.NewFileReference("docs/a b.md", domain.NewWholeFileSelection())
	b, err := domain.FreezeFileAttachment(ref, "\ufeff@not-recursive\r\n<safe>&\nend")
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a, b})
	if err != nil {
		t.Fatal(err)
	}
	raw := `inspect @src/a.go#L2-8 and @"docs/a b.md"`
	text := raw
	if skill {
		text = "$qa:demo " + text
	}
	u, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000011", text)
	u, err = u.WithFileAttachments(files)
	if err != nil {
		t.Fatal(err)
	}
	if skill {
		source, _ := domain.NewSkillSource("plugin", "qa", "codex")
		attachment, _ := domain.NewSkillAttachment(source, "qa:demo", domain.SkillDigest("source"), raw, "Frozen skill instructions")
		u, err = u.WithSkillAttachment(attachment)
		if err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func TestFileInputFixedNativeBlocksPrepareDiscardFinalizeAndRestore(t *testing.T) {
	for _, skill := range []bool{false, true} {
		t.Run(fmt.Sprint(skill), func(t *testing.T) {
			instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			c := testConversation(t, instance)
			u := frozenFileInput(t, skill)
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
			value, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope}))
			if err != nil {
				t.Fatal(err)
			}
			restored := value.(*Conversation)
			sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testAnthropicToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			before, err := c.compileSamplingRequest(c.history.Entries(), sampling)
			if err != nil {
				t.Fatal(err)
			}
			after, err := restored.compileSamplingRequest(restored.history.Entries(), sampling)
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
			entries := restored.history.Entries()
			entries[0].InputMetadata.Files[0].Attachments = domain.FileAttachments{}
			if validateNativeHistory(entries) == nil || restored.ProjectHistory().Turns[0].Messages[0].Files.Len() != 2 {
				t.Fatal("clone leaked or invalid metadata accepted")
			}
		})
	}
}
