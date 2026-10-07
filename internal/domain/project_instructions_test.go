package domain

import (
	"math"
	"strings"
	"testing"
)

func TestProjectInstructionsSnapshotIsDeterministicAndImmutable(t *testing.T) {
	t.Parallel()
	documents := []ProjectInstructionDocument{
		mustProjectInstructionDocument(t, "AGENTS.md", "root rule"),
		mustProjectInstructionDocument(t, "nested/CLAUDE.md", "nested rule"),
	}
	first, err := NewProjectInstructionsSnapshot(documents, 4096)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewProjectInstructionsSnapshot(documents, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision() != second.Revision() || string(first.CanonicalJSON()) != string(second.CanonicalJSON()) {
		t.Fatal("equal project instruction inputs produced different snapshots")
	}
	documents[0] = ProjectInstructionDocument{}
	got := first.Documents()
	got[0] = ProjectInstructionDocument{}
	canonical := first.CanonicalJSON()
	canonical[0] = '['
	if first.Documents()[0].Source() != "AGENTS.md" || first.CanonicalJSON()[0] != '{' {
		t.Fatal("snapshot leaked caller-owned memory")
	}
	clone, err := first.Clone()
	if err != nil || clone.Revision() != first.Revision() {
		t.Fatalf("clone = %#v, %v", clone, err)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectInstructionsSnapshotValidatesSourcesAndOrder(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", "/AGENTS.md", "../AGENTS.md", "nested\\AGENTS.md", "notes.md", "a//AGENTS.md"} {
		if _, err := NewProjectInstructionDocument(source, "rule"); err == nil {
			t.Fatalf("source %q unexpectedly accepted", source)
		}
	}
	invalidUTF8 := string([]byte{0xff})
	if _, err := NewProjectInstructionDocument("AGENTS.md", invalidUTF8); err == nil {
		t.Fatal("invalid UTF-8 unexpectedly accepted")
	}
	unordered := []ProjectInstructionDocument{
		mustProjectInstructionDocument(t, "one/AGENTS.md", "one"),
		mustProjectInstructionDocument(t, "two/AGENTS.md", "two"),
	}
	if _, err := NewProjectInstructionsSnapshot(unordered, 4096); err == nil {
		t.Fatal("unrelated source directories unexpectedly accepted")
	}
	duplicate := []ProjectInstructionDocument{
		mustProjectInstructionDocument(t, "AGENTS.md", "one"),
		mustProjectInstructionDocument(t, "AGENTS.md", "two"),
	}
	if _, err := NewProjectInstructionsSnapshot(duplicate, 4096); err == nil {
		t.Fatal("duplicate source unexpectedly accepted")
	}
}

func TestProjectInstructionsSnapshotAppliesVisibleByteBudgetAtUTF8Boundary(t *testing.T) {
	t.Parallel()
	document := mustProjectInstructionDocument(t, "AGENTS.md", strings.Repeat("界", 20))
	full, err := NewProjectInstructionsSnapshot([]ProjectInstructionDocument{document}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	limit := len(full.RenderedText()) - 1
	truncated, err := NewProjectInstructionsSnapshot([]ProjectInstructionDocument{document}, limit)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated.Truncated() || len(truncated.RenderedText()) > limit {
		t.Fatalf("truncated=%t bytes=%d limit=%d", truncated.Truncated(), len(truncated.RenderedText()), limit)
	}
	if got := truncated.Documents()[0].Content(); !strings.HasPrefix(document.Content(), got) || len(got)%len("界") != 0 {
		t.Fatalf("content was not truncated at a UTF-8 boundary: %q", got)
	}
	exact, err := NewProjectInstructionsSnapshot(truncated.Documents(), len(truncated.RenderedText()))
	if err != nil {
		t.Fatal(err)
	}
	if exact.Truncated() {
		t.Fatal("exact visible byte limit unexpectedly truncated")
	}
}

func TestEmptyProjectInstructionsSnapshotIsExplicitAndSilent(t *testing.T) {
	t.Parallel()
	snapshot, err := NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HasDocuments() || snapshot.RenderedText() != "" || snapshot.Truncated() {
		t.Fatalf("empty snapshot = %#v", snapshot)
	}
	if snapshot.Revision() == "" || len(snapshot.CanonicalJSON()) == 0 {
		t.Fatal("empty snapshot lacks a stable identity")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectInstructionsSnapshotRejectsInvalidByteLimits(t *testing.T) {
	t.Parallel()
	if _, err := NewEmptyProjectInstructionsSnapshot(MaxProjectInstructionsBytes); err != nil {
		t.Fatalf("hard limit rejected: %v", err)
	}
	for _, byteLimit := range []int{0, -1, MaxProjectInstructionsBytes + 1, math.MaxInt} {
		if _, err := NewEmptyProjectInstructionsSnapshot(byteLimit); err == nil {
			t.Fatalf("byte limit %d unexpectedly accepted", byteLimit)
		}
	}

	valid, err := NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	valid.byteLimit = MaxProjectInstructionsBytes + 1
	if err := valid.Validate(); err == nil {
		t.Fatal("snapshot with an excessive stored byte limit unexpectedly validated")
	}
}

func mustProjectInstructionDocument(t *testing.T, source string, content string) ProjectInstructionDocument {
	t.Helper()
	document, err := NewProjectInstructionDocument(source, content)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
