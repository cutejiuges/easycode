package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func completionSkillEntry(t testing.TB, name, description string) domain.SkillListEntry {
	t.Helper()
	source, err := domain.NewSkillSource("project", ".", "easycode")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := domain.NewSkillMetadata(source, name, description, "[args]", true, false)
	if err != nil {
		t.Fatal(err)
	}
	return domain.SkillListEntry{Name: name, Source: source, Metadata: metadata, Status: "available"}
}

func completionNames(c *completionCatalog, matches []completionMatch) []string {
	var names []string
	for _, match := range matches {
		names = append(names, c.entries[match.index].id.name)
	}
	return names
}

func TestCompletionCatalogPermissionsAndDeterminism(t *testing.T) {
	manual := completionSkillEntry(t, "manual", "Manual only")
	manual.Metadata.DisableModelInvocation = true
	model := completionSkillEntry(t, "model", "Model only")
	model.Metadata.UserInvocable = false
	invalid := completionSkillEntry(t, "bad", "Broken")
	invalid.Status = "invalid"
	shadow := completionSkillEntry(t, "shadow", "Shadowed")
	shadow.Status = "shadowed"
	mismatch := completionSkillEntry(t, "mismatch", "Wrong metadata")
	mismatch.Metadata.Name = "other"
	entries := []domain.SkillListEntry{manual, model, invalid, shadow, mismatch, completionSkillEntry(t, "cache", "Skill collision")}
	first, err := newCompletionCatalog(entries)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	second, err := newCompletionCatalog(entries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("enumeration changed catalog")
	}
	slash, _ := findCompletionTarget("/", 1)
	dollar, _ := findCompletionTarget("$", 1)
	if got := completionNames(first, first.match(slash)); !reflect.DeepEqual(got, []string{"cache", "compact", "context", "hooks", "skills", "manual"}) {
		t.Fatal(got)
	}
	if got := completionNames(first, first.match(dollar)); !reflect.DeepEqual(got, []string{"cache", "manual"}) {
		t.Fatal(got)
	}
	for _, e := range entries {
		if explicitSkillEntry(e) != (e.Name == "manual" || e.Name == "cache") {
			t.Fatal("picker eligibility mismatch", e.Name)
		}
	}
}

func TestCompletionTargetBoundaries(t *testing.T) {
	for _, tc := range []struct {
		text         string
		cursor       int
		query, token string
		ok           bool
	}{
		{"/", 1, "", "/", true}, {"$", 1, "", "$", true},
		{" \u3000/review args\n尾部  ", 7, "re", "/review", true},
		{"/ReView", 7, "review", "/ReView", true},
		{"/review args", 8, "", "", false}, {"text $review", 12, "", "", false},
		{"/tmp/file", 4, "", "", false}, {"/file.txt", 5, "", "", false},
		{"```\n/review", 8, "", "", false}, {"/中文", 1, "", "", false},
		{"/" + strings.Repeat("a", 64), 65, strings.Repeat("a", 64), "/" + strings.Repeat("a", 64), true},
		{"/" + strings.Repeat("a", 65), 1, "", "", false}, {"/a", 0, "", "", false},
		{"/a", -1, "", "", false}, {"/a", 3, "", "", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			got, ok := findCompletionTarget(tc.text, tc.cursor)
			if ok != tc.ok || ok && (got.query != tc.query || got.token != tc.token) {
				t.Fatalf("target=%+v ok=%v", got, ok)
			}
		})
	}
}

func TestCompletionMatchTiers(t *testing.T) {
	entries := []domain.SkillListEntry{
		completionSkillEntry(t, "rev", "Exact"), completionSkillEntry(t, "review", "Prefix"),
		completionSkillEntry(t, "resolve", "Subsequence"), completionSkillEntry(t, "audit", "REV description"),
	}
	c, err := newCompletionCatalog(entries)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := findCompletionTarget("$rev", 4)
	if got := completionNames(c, c.match(target)); !reflect.DeepEqual(got, []string{"rev", "review", "resolve", "audit"}) {
		t.Fatal(got)
	}
	target, _ = findCompletionTarget("$zzzz", 5)
	if len(c.match(target)) != 0 {
		t.Fatal("unmatched query yielded choices")
	}
}

func TestCompletionReplacementPreservesTextAndHistory(t *testing.T) {
	candidate := completionCandidate{id: completionIdentity{kind: completionSkill, name: "review"}, hint: "never inserted"}
	for _, tc := range []struct {
		text, want         string
		cursor, wantCursor int
	}{
		{"  /rev old\n尾部  ", "  /review old\n尾部  ", 5, 10},
		{"$re", "$review ", 3, 8}, {"/re\targs", "/review\targs", 3, 8},
		{"/re\nargs", "/review\nargs", 3, 8}, {"/re\u3000args", "/review\u3000args", 3, 10},
	} {
		c := composerWithText(t, tc.text)
		c.moveTo(tc.cursor)
		target, ok := findCompletionTarget(c.text, c.cursor)
		if !ok {
			t.Fatal(tc)
		}
		c.history.add("old")
		if err := c.complete(target, candidate); err != nil {
			t.Fatal(err)
		}
		if c.text != tc.want || c.cursor != tc.wantCursor || c.history.entries[0] != "old" {
			t.Fatalf("text=%q cursor=%d", c.text, c.cursor)
		}
	}
	// 用既有小布局避免为了拒绝路径预建 4 MiB 字素布局。
	c := newComposer()
	c.text = "/r " + strings.Repeat("x", protocol.MaxInputBytes-3)
	c.cursor = 2
	before := c
	target, _ := findCompletionTarget(c.text, c.cursor)
	if err := c.complete(target, candidate); err == nil || !reflect.DeepEqual(c, before) {
		t.Fatal("oversized completion changed draft")
	}
}

func TestCompletionAcceptsExactInputLimit(t *testing.T) {
	tail := strings.Repeat("x", protocol.MaxInputBytes-len("/review "))
	c := newComposer()
	// 只预置正文，避免测试准备先构建一份即将被替换的大布局。
	c.text, c.cursor = "/r "+tail, 2
	target, ok := findCompletionTarget(c.text, c.cursor)
	if !ok {
		t.Fatal("valid first token rejected")
	}
	if err := c.complete(target, completionCandidate{id: completionIdentity{kind: completionSkill, name: "review"}}); err != nil {
		t.Fatal(err)
	}
	if len(c.text) != protocol.MaxInputBytes || c.cursor != len("/review ") || strings.TrimPrefix(c.text, "/review ") != tail {
		t.Fatal("exact-size completion was truncated or moved the cursor")
	}
}

func completionModel(t testing.TB, text string) Model {
	t.Helper()
	m := NewModel(context.Background(), "test", &fakeChatSession{}).WithSkills([]domain.SkillListEntry{completionSkillEntry(t, "review", "Review changes")})
	if err := m.composer.replace(0, 0, text); err != nil {
		t.Fatal(err)
	}
	m.prepareCompletion()
	return m
}

func TestCompletionSelectionAndCopyIsolation(t *testing.T) {
	m := completionModel(t, "/")
	original := m
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.completion.selected != 1 || original.completion.selected != 0 || original.composer.text != "/" {
		t.Fatal("value copies mutated")
	}
	selected := m.completion.catalog.entries[m.completion.matches[m.completion.selected].index].id
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("co")})
	if m.completion.catalog.entries[m.completion.matches[m.completion.selected].index].id != selected {
		t.Fatal("selection drifted")
	}
	if len(original.completion.matches) != 6 {
		t.Fatal("old matches were overwritten")
	}
	before := m.completion
	m = updateModel(t, m, struct{}{})
	if &before.matches[0] != &m.completion.matches[0] || before.view != m.completion.view || before.generation != m.completion.generation {
		t.Fatal("unrelated update rebuilt completion")
	}
}

func TestCompletionCatalogBounds(t *testing.T) {
	var entries []domain.SkillListEntry
	for i := 0; i < domain.MaxSkills; i++ {
		entries = append(entries, completionSkillEntry(t, strings.Repeat("a", i/26+1)+string(rune('a'+i%26)), "Description"))
	}
	c, err := newCompletionCatalog(entries)
	if err != nil || len(c.entries) != maxCompletionCandidates || c.bytes > maxCompletionBytes {
		t.Fatal("valid bounded catalog rejected", err)
	}
	entries = append(entries, completionSkillEntry(t, "overflow", "Overflow"))
	if c, err := newCompletionCatalog(entries); err == nil || c != nil {
		t.Fatal("published partial oversized catalog")
	}
	e := completionSkillEntry(t, "huge", "Large source")
	e.Source.Directory = strings.Repeat("x", maxCompletionBytes)
	e.Metadata.Source = e.Source
	if c, err := newCompletionCatalog([]domain.SkillListEntry{e}); err == nil || c != nil {
		t.Fatal("large source escaped budget")
	}
}
