package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
)

type filePreparationFixture struct {
	contents map[string]string
	reads    []string
	consumed int
	fail     string
}

func (f *filePreparationFixture) ReadWorkspaceFile(ctx context.Context, path string, limit int) (string, int, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	f.reads = append(f.reads, path)
	content := f.contents[path]
	if len(content) > limit {
		return "", 0, domain.NewFileFailure(domain.FileLimit)
	}
	f.consumed += len(content)
	if path == f.fail {
		return "", len(content), domain.NewFileFailure(domain.FileChanged)
	}
	return content, len(content), nil
}

func TestFilePreparationValidatesBeforeIOAndPreservesIntent(t *testing.T) {
	f := &filePreparationFixture{contents: map[string]string{"a": "a"}}
	for _, text := range []string{"@a @../bad", "@a @b @c @d @e @f @g @h @i", `@a @"bad`} {
		draft, err := domain.NewInteractiveInputDraft(text)
		if err == nil {
			t.Fatal("invalid draft accepted")
		}
		if _, err := prepareWorkspaceFiles(context.Background(), draft, f); err == nil {
			t.Fatal("invalid draft prepared")
		}
	}
	if len(f.reads) != 0 {
		t.Fatal("read a valid prefix before rejecting invalid input")
	}
	literal, _ := domain.NewLiteralInputDraft("@../literal")
	if files, err := prepareWorkspaceFiles(context.Background(), literal, f); err != nil || files.Len() != 0 || len(f.reads) != 0 {
		t.Fatal("literal activated", err)
	}
}

func TestFilePreparationGroupsSourcesAndKeepsReferenceOrder(t *testing.T) {
	f := &filePreparationFixture{contents: map[string]string{"a": "first\r\nsecond\nlast", "b": "@not-recursive\n"}}
	draft, err := domain.NewInteractiveInputDraft("@a#L2 @b @a#L1 @a#L2")
	if err != nil {
		t.Fatal(err)
	}
	files, err := prepareWorkspaceFiles(context.Background(), draft, f)
	if err != nil || files.Len() != 3 || strings.Join(f.reads, ",") != "a,b" {
		t.Fatal("source grouping failed", err, f.reads)
	}
	items := files.Items()
	if items[0].Content() != "second\n" || items[1].Content() != "@not-recursive\n" || items[2].Content() != "first\r\n" {
		t.Fatal("original order or line bytes lost")
	}
	f.contents["a"] = "changed"
	if items[0].Content() != "second\n" {
		t.Fatal("prepared content changed")
	}
}

func TestFilePreparationRejectsPartialFailureAndCountsSourceBudget(t *testing.T) {
	draft, _ := domain.NewInteractiveInputDraft("@a @b")
	f := &filePreparationFixture{contents: map[string]string{"a": "ok", "b": "bad"}, fail: "b"}
	if files, err := prepareWorkspaceFiles(context.Background(), draft, f); err == nil || files.Len() != 0 || f.consumed != 5 {
		t.Fatal("partial input delivered or failure bytes reset", err)
	}
	large := strings.Repeat("x", domain.MaxFileSourceBytes)
	f = &filePreparationFixture{contents: map[string]string{"a": large, "b": large, "c": large, "d": large, "e": "one"}}
	draft, _ = domain.NewInteractiveInputDraft("@a @b @c @d @e")
	if files, err := prepareWorkspaceFiles(context.Background(), draft, f); err == nil || files.Len() != 0 || f.consumed != domain.MaxFileInputSourceBytes {
		t.Fatal("source budget was bypassed", err, f.consumed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareWorkspaceFiles(ctx, draft, f); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
