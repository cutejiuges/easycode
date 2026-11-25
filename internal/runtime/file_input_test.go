package runtime

import (
	"testing"

	"easycode/internal/domain"
)

func runtimeFileInputs(t *testing.T, text string) []domain.UserInput {
	t.Helper()
	ref, err := domain.NewFileReference("file", domain.NewWholeFileSelection())
	requireNoError(t, err)
	a, err := domain.FreezeFileAttachment(ref, "FROZEN_RUNTIME\n")
	requireNoError(t, err)
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a})
	requireNoError(t, err)
	input, err := domain.NewUserInput("01900000-0000-7000-8000-000000000001", text)
	requireNoError(t, err)
	input, err = input.WithFileAttachments(files)
	requireNoError(t, err)
	return []domain.UserInput{input}
}
