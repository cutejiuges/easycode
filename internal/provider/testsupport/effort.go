package testsupport

import (
	"bytes"
	"os"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// EffortGoldenRecord 核对实际请求的 effort、操作上限及三种真实缓存证据。
func EffortGoldenRecord(t *testing.T, name string, body codec.CanonicalJSON, evidence provider.RequestEvidence, selected domain.InferenceSettings, purpose domain.RequestPurpose) []byte {
	t.Helper()
	var wire struct {
		Reasoning struct {
			Effort domain.EffortLevel `json:"effort"`
		} `json:"reasoning"`
		OutputConfig struct {
			Effort domain.EffortLevel `json:"effort"`
		} `json:"output_config"`
		MaxTokens uint64 `json:"max_tokens"`
		MaxOutput uint64 `json:"max_output_tokens"`
	}
	if codec.Unmarshal(body.Bytes(), &wire) != nil || (selected.Family() == domain.ProviderAnthropic && wire.OutputConfig.Effort != selected.Effort() || selected.Family() == domain.ProviderOpenAI && wire.Reasoning.Effort != selected.Effort()) {
		t.Fatal("body did not use captured effort", string(body.Bytes()))
	}
	if purpose == domain.RequestSummary && wire.MaxTokens+wire.MaxOutput != 4096 {
		t.Fatal("summary cap changed")
	}
	if purpose == domain.RequestSample && wire.MaxTokens+wire.MaxOutput != 8192 {
		t.Fatal("ordinary cap changed")
	}
	if bytes.Contains(body.Bytes(), []byte("configuration_update")) || bytes.Contains(body.Bytes(), []byte("selection_revision")) {
		t.Fatal("process selection leaked to wire")
	}
	record, err := codec.MarshalStable(struct {
		Name       string `json:"name"`
		Request    string `json:"request"`
		Native     string `json:"native"`
		Properties string `json:"properties"`
	}{name, domain.ContentFingerprint(body.Bytes()), evidence.NativeFingerprint(), domain.ContentFingerprint(evidence.Properties().Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	return append(record, '\n')
}

func CheckEffortGolden(t *testing.T, actual []byte) {
	t.Helper()
	path := "testdata/runtime_effort_requests.golden.jsonl"
	if os.Getenv("EASYCODE_UPDATE_EFFORT_GOLDENS") == "1" {
		if err := os.WriteFile(path, actual, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, actual) {
		t.Fatal("runtime policy body/properties/native golden mismatch", err)
	}
}
