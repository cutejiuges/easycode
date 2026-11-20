package runtime

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func rewriteCandidateFixture(t *testing.T, r *Runtime, effective tool.Invocation) (func() int, func() int, string) {
	t.Helper()
	switch effective.Capability() {
	case tool.CapabilityPatch:
		v, _ := effective.Patch()
		input := v.Input()
		rs := input.Replacements()
		base, final := []byte(rs[0].OldText()), []byte(rs[0].NewText())
		evidence, _ := tool.NewFileBaseEvidence(tool.NewContentDigest(base), int64(len(base)), 0o644, 1, 2)
		metadata, _ := tool.NewPatchResultMetadata(input.FilePath(), len(rs), 1, 1)
		fingerprint, _ := tool.PatchFingerprint(input, evidence, final)
		candidate, err := tool.NewPatchCandidate(v, evidence, final, "--- old\n+++ rewritten\n", fingerprint, metadata)
		if err != nil {
			t.Fatal(err)
		}
		p, e := &fixedPatchPreparer{candidate: candidate}, &fixedPatchExecutor{}
		r.tools.PreparePatch, r.tools.Patch = p, e
		return func() int { return p.calls }, func() int { return e.calls }, string(candidate.Fingerprint())
	case tool.CapabilityWrite:
		v, _ := effective.Write()
		root, _ := tool.NewDirectoryEvidence(".", 1, 2, 0o700)
		candidate, err := tool.NewWriteCandidate(v, tool.WriteCreate, tool.FileBaseEvidence{}, []tool.DirectoryEvidence{root}, nil, "+ rewritten\n")
		if err != nil {
			t.Fatal(err)
		}
		p, e := &fixedWritePreparer{candidate: candidate}, &fixedWriteExecutor{}
		r.tools.PrepareWrite, r.tools.Write = p, e
		return func() int { return p.calls }, func() int { return e.calls }, string(candidate.Fingerprint())
	case tool.CapabilityExec:
		v, _ := effective.Exec()
		shell, _ := tool.NewShellProfile("sh", "/bin/sh")
		environment, _ := tool.NewEnvironmentPolicy("minimal", []string{"PATH"})
		sandbox, _ := tool.NewSandboxProfile("workspace", false, true)
		candidate, err := tool.NewExecCandidate(v, shell, environment, sandbox)
		if err != nil {
			t.Fatal(err)
		}
		e := &fixedExecComponents{candidate: candidate}
		p := &rewriteExecPreparer{candidate: candidate}
		r.tools.PrepareExec, r.tools.Exec = p, e
		return func() int { return p.calls }, func() int { return e.calls }, string(candidate.Fingerprint())
	default:
		t.Fatal("unsupported fixture capability")
		return nil, nil, ""
	}
}

type rewriteExecPreparer struct {
	candidate tool.ExecCandidate
	calls     int
}

func (p *rewriteExecPreparer) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	p.calls++
	return p.candidate, nil
}

func rewrittenFixtureInput(t *testing.T, original tool.Invocation, second bool) domain.HookToolInput {
	t.Helper()
	target, content := "rewritten.txt", "rewritten"
	if second {
		target, content = "final.txt", "final"
	}
	var input domain.HookToolInput
	var err error
	switch original.Capability() {
	case tool.CapabilityPatch:
		input, err = domain.NewHookPatchInput(target, []domain.HookReplacement{{OldText: "old\n", NewText: content + "\n", LineBased: true}})
	case tool.CapabilityWrite:
		input, err = domain.NewHookWriteInput(target, content)
	case tool.CapabilityExec:
		input, err = domain.NewHookExecInput("printf "+content, "subdir", 5000)
	}
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func countRewriteFact(t *testing.T, j *fakeJournal, kind session.EventKind) int {
	t.Helper()
	count := 0
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == kind {
				count++
			}
		}
	}
	return count
}
