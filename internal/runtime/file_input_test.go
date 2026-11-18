package runtime

import (
	"testing"

	"easycode/internal/domain"
)

func runtimeFileInputs(t *testing.T, text string) []domain.UserInput {
	t.Helper()
	ref, err := domain.NewFileReference("file", domain.NewWholeFileSelection())
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.FreezeFileAttachment(ref, "FROZEN_RUNTIME\n")
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a})
	if err != nil {
		t.Fatal(err)
	}
	input, err := domain.NewUserInput("01900000-0000-7000-8000-000000000001", text)
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithFileAttachments(files)
	if err != nil {
		t.Fatal(err)
	}
	return []domain.UserInput{input}
}
