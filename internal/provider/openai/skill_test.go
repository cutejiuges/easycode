package openai

import (
	"bytes"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"os"
	"strings"
	"testing"
)

func TestSkillAttachmentFrozenNativeRestore(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	source, _ := domain.NewSkillSource("project", ".", "easycode")
	a, _ := domain.NewSkillAttachment(source, "demo", domain.SkillDigest("source"), "args", "Frozen skill instructions")
	user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000011", " /demo args ")
	user, err = user.WithSkillAttachment(a)
	if err != nil {
		t.Fatal(err)
	}
	association, _ := domain.NewInputAssociation("01900000-0000-7000-8000-000000000010", domain.InputInitial, 0, []domain.InputID{user.ID()})
	prepared, err := conversation.PrepareUserInputs(association, []domain.UserInput{user})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := prepared.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/skill_input.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(golden), envelope.Payload()) {
		t.Fatalf("skill native golden mismatch: %s", envelope.Payload())
	}
	if len(conversation.ProjectHistory().Turns) != 0 {
		t.Fatal("prepared attachment leaked before finalize")
	}
	if err := prepared.Discard(); err != nil {
		t.Fatal(err)
	}
	prepared, err = conversation.PrepareUserInputs(association, []domain.UserInput{user})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope}))
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	m := restored.ProjectHistory().Turns[0].Messages[0]
	if m.Text != user.Text() || !m.HasSkill || m.Skill != a {
		t.Fatal("raw input and attachment projection mismatch")
	}
	catalogMetadata, _ := domain.NewSkillMetadata(source, "demo", "on demand", "", true, false)
	catalog, _ := domain.NewSkillCatalog([]domain.SkillMetadata{catalogMetadata})
	sampling, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	sampling, _ = sampling.WithSkillCatalog(catalog)
	live, err := conversation.compileSamplingRequest(conversation.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live.Bytes(), recovered.Bytes()) || domain.ContentFingerprint(live.Bytes()) != domain.ContentFingerprint(recovered.Bytes()) {
		t.Fatal("restore changed native request")
	}
	text := string(live.Bytes())
	if strings.Count(text, "Frozen skill instructions") != 1 || strings.Index(text, "on demand") > strings.Index(text, "Frozen skill instructions") {
		t.Fatal("catalog or attachment ordering")
	}
	entries := restored.history.snapshot()
	entries[0].InputMetadata.Skills[0].Attachment.Content = "tampered"
	if validateNativeHistory(entries) == nil {
		t.Fatal("attachment/native mismatch accepted")
	}
	if restored.ProjectHistory().Turns[0].Messages[0].Skill.Content != a.Content {
		t.Fatal("metadata clone aliased")
	}
}
