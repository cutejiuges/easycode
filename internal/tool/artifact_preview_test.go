package tool

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestArtifactPreviewFrozenGoldenAndSharedBudget(t *testing.T) {
	for _, state := range []ArtifactSaveStatus{ArtifactNotNeeded, ArtifactComplete, ArtifactPartial, ArtifactUnavailable} {
		t.Run(string(state), func(t *testing.T) {
			data := "small output"
			if state != ArtifactNotNeeded {
				data = strings.Repeat("0123456789", 60000)
			}
			var out, stderr ExecOutputBuffer
			_, _ = out.Write([]byte(data))
			_, _ = stderr.Write([]byte(data))
			metadata, _ := NewExecResultMetadata("", 2, true, 1, out.Snapshot().Stats(), stderr.Snapshot().Stats(), false, false, false)
			makeSaving := func(id ArtifactID, stream ArtifactStream) ArtifactSaving {
				reason := ""
				var d ArtifactDescriptor
				if state == ArtifactUnavailable {
					reason = "quota_exceeded"
				}
				if state == ArtifactComplete || state == ArtifactPartial {
					saved := int64(len(data))
					if state == ArtifactPartial {
						saved = 500000
						reason = "write_failed"
					}
					var err error
					d, err = NewArtifactDescriptor(id, domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"), InvocationID("018f1d8a-7b5c-7def-8123-456789abcdee"), stream, saved, int64(len(data)), strings.Repeat("a", 64), state == ArtifactComplete)
					if err != nil {
						t.Fatal(err)
					}
				}
				value, err := NewArtifactSaving(state, reason, d)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			var err error
			metadata, err = metadata.WithOutputSaving(makeSaving("0123456789abcdef0123456789abcdef", ArtifactStdout), makeSaving("1123456789abcdef0123456789abcdef", ArtifactStderr))
			if err != nil {
				t.Fatal(err)
			}
			preview, truncated, err := RenderExecOutput(out.Snapshot(), stderr.Snapshot(), metadata, ExecModelPreviewBytes)
			if err != nil || len(preview.Text())+64 > ExecModelPreviewBytes || truncated != (state != ArtifactNotNeeded) {
				t.Fatalf("shared preview budget: %v", err)
			}
			if ExecOutputNeedsSaving(out.Snapshot(), stderr.Snapshot(), ExecModelPreviewBytes) != (state != ArtifactNotNeeded) {
				t.Fatal("saving decision disagreed with preview omission")
			}
			digest := sha256.Sum256([]byte(preview.Text()))
			actual := fmt.Sprintf("bytes=%d\ntruncated=%t\nsha256=%x\nstdout=%s\nstderr=%s\n", len(preview.Text()), truncated, digest, renderArtifactSaving("stdout", metadata.StdoutSaving()), renderArtifactSaving("stderr", metadata.StderrSaving()))
			want, err := os.ReadFile("testdata/artifact_preview_" + string(state) + ".golden")
			if err != nil || string(want) != actual {
				t.Fatalf("artifact preview golden: actual=%q", actual)
			}
		})
	}
}

func TestSmallExecOutputNeedsSavingAtActualAllowance(t *testing.T) {
	var out, stderr ExecOutputBuffer
	_, _ = out.Write([]byte(strings.Repeat("hello", 2000)))
	if ExecOutputNeedsSaving(out.Snapshot(), stderr.Snapshot(), ExecModelPreviewBytes) || !ExecOutputNeedsSaving(out.Snapshot(), stderr.Snapshot(), SafetyResultBytes) {
		t.Fatal("saving decision ignored actual allowance")
	}
	metadata, _ := NewExecResultMetadata("", 7, true, 1, out.Snapshot().Stats(), stderr.Snapshot().Stats(), false, false, false)
	unavailable, _ := NewArtifactSaving(ArtifactUnavailable, "quota_exceeded", ArtifactDescriptor{})
	metadata, _ = metadata.WithOutputSaving(unavailable, unavailable)
	preview, cut, err := RenderExecOutput(out.Snapshot(), stderr.Snapshot(), metadata, SafetyResultBytes)
	if err != nil || !cut || len(preview.Text())+128 > SafetyResultBytes || !strings.Contains(preview.Text(), "exit_code: 7") || !strings.Contains(preview.Text(), "quota_exceeded") {
		t.Fatalf("bounded outcome: %v", err)
	}
}
