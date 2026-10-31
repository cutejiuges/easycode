package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"easycode/internal/domain"
)

func TestResumeRejectsBeforeRepair(t *testing.T) {
	for _, reason := range []string{"model", "native"} {
		t.Run(reason, func(t *testing.T) {
			service := newTestSessionService(t)
			defer service.close()
			factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
			created, err := service.create(context.Background(), factory, "responses", "gpt-test", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := created.writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			path, err := service.repository.JournalPath(created.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			prefix, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			original := append(append([]byte(nil), prefix...), []byte(`{"torn":`)...)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			model := "gpt-test"
			if reason == "model" {
				model = "other"
			} else {
				factory.restoreErr = errors.New("invalid native source")
			}
			if _, err := service.resume(context.Background(), factory, "responses", model, created.identity.ThreadID); err == nil {
				t.Fatal("invalid resume accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("failed validation repaired journal")
			}
			if factory.networkCalls.Load() != 0 {
				t.Fatal("restore used network")
			}
			factory.restoreErr = nil
			resumed, err := service.resume(context.Background(), factory, "responses", "gpt-test", created.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			if !resumed.repair.Repaired {
				t.Fatal("valid resume did not repair")
			}
			if err := resumed.writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, err = os.ReadFile(path)
			if err != nil || !bytes.Equal(after, prefix) {
				t.Fatal("valid repair changed committed prefix")
			}
		})
	}
}
