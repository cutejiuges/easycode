package codec_test

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
	"time"

	"easycode/internal/codec"
	cache "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/session"
	"github.com/bytedance/sonic"
)

func TestCodecIndependentOfThirdPartyGlobals(t *testing.T) {
	if os.Getenv("EASYCODE_CODEC_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCodecIndependentOfThirdPartyGlobals$")
		cmd.Env = append(os.Environ(), "EASYCODE_CODEC_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated codec regression: %v\n%s", err, out)
		}
		return
	}
	snapshot := func() ([]byte, string, string) {
		canonical, err := codec.MarshalCanonical(map[string]string{"z": "<>&", "a": "stable"}, 1024)
		if err != nil {
			t.Fatal(err)
		}
		segment, err := cache.NewSegment("system", cache.StabilityStable, "current", canonical)
		if err != nil {
			t.Fatal(err)
		}
		draft, err := session.NewTurnFailedDraft(domain.TurnID("00000000-0002-7000-8000-000000000003"), session.TurnFailedPayload{Code: "failed", Message: "<safe>"})
		if err != nil {
			t.Fatal(err)
		}
		record, err := session.BuildRecord(session.Identity{SessionID: "00000000-0000-7000-8000-000000000001", ThreadID: "00000000-0001-7000-8000-000000000002"}, draft, 1, time.Unix(1, 0), 1, 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		sealed, _, err := session.EncodeRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		return canonical.Bytes(), segment.Fingerprint(), sealed.Checksum
	}
	before, fingerprint, checksum := snapshot()
	sonic.ConfigStd = sonic.Config{}.Froze()
	sonic.ConfigDefault = sonic.Config{EscapeHTML: false}.Froze()
	sonic.ConfigFastest = sonic.Config{}.Froze()
	after, nextFingerprint, nextChecksum := snapshot()
	if !bytes.Equal(before, after) || fingerprint != nextFingerprint || checksum != nextChecksum {
		t.Fatal("third-party defaults changed canonical bytes, fingerprint or checksum")
	}
}
