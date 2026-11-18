package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestRuntimeEffortRequestPolicyGolden(t *testing.T) {
	var actual bytes.Buffer
	for _, mode := range []string{"responses"} {

		startup, _ := domain.NewOpenAIInference("high", "auto", 8192)
		for _, cache := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled} {
			policy, _ := domain.NewCachePolicy(cache, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
			instance, err := New(Config{Inference: startup, CachePolicy: policy, BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key")})
			if err != nil {
				t.Fatal(err)
			}
			c := testConversation(t, instance)
			c.history.commit(testInputEntry(t, NewUserItem("same candidate input")))
			choices := []domain.EffortChoice{domain.NewResetEffortChoice(), domain.NewAutomaticEffortChoice()}
			for _, level := range []domain.EffortLevel{"low", "high"} {
				choice, _ := domain.NewExplicitEffortChoice(level)
				choices = append(choices, choice)
			}
			for _, choice := range choices {
				selected, err := choice.Apply(startup)
				if err != nil {
					t.Fatal(err)
				}
				input, err := testSamplingInput(t, selected).WithToolCatalog(testOpenAIToolCatalog(t))
				if err != nil {
					t.Fatal(err)
				}
				ordinary, err := c.planSamplingRequest(c.history.snapshot(), input)
				if err != nil {
					t.Fatal(err)
				}
				compact, _ := provider.NewCompactionInput(input, "preserve failures", 4096)
				summary, err := c.planCompactionRequest(compact)
				if err != nil {
					t.Fatal(err)
				}
				for _, plan := range []samplingRequestPlan{ordinary, summary} {
					body, err := plan.body()
					if err != nil {
						t.Fatal(err)
					}
					evidence, err := plan.evidence()
					if err != nil {
						t.Fatal(err)
					}
					var wire struct {
						Reasoning struct {
							Effort  domain.EffortLevel `json:"effort"`
							Summary string             `json:"summary"`
						} `json:"reasoning"`
						OutputConfig struct {
							Effort domain.EffortLevel `json:"effort"`
						} `json:"output_config"`
						Thinking  json.RawMessage `json:"thinking"`
						MaxTokens uint64          `json:"max_tokens"`
						MaxOutput uint64          `json:"max_output_tokens"`
						Store     bool            `json:"store"`
					}
					if codec.Unmarshal(body.Bytes(), &wire) != nil || wire.Reasoning.Effort != selected.Effort() {
						t.Fatal("body did not use captured effort", string(body.Bytes()))
					}
					if instance.config.Inference != startup || selected.MaxOutputTokens() != startup.MaxOutputTokens() {
						t.Fatal("operation mutated startup")
					}
					if plan.purpose == domain.RequestSummary && wire.MaxTokens+wire.MaxOutput != 4096 {
						t.Fatal("summary cap changed")
					}
					if plan.purpose == domain.RequestSample && wire.MaxTokens+wire.MaxOutput != 8192 {
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
					}{fmt.Sprintf("%s/%s/%s/%s/%s", mode, cache, choice.Mode(), choice.Level(), plan.purpose), domain.ContentFingerprint(body.Bytes()), evidence.nativeFingerprint, domain.ContentFingerprint(evidence.properties.Bytes())})
					if err != nil {
						t.Fatal(err)
					}
					actual.Write(record)
					actual.WriteByte('\n')
				}
			}
			if err := instance.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := "testdata/runtime_effort_requests.golden.jsonl"
	if os.Getenv("EASYCODE_UPDATE_EFFORT_GOLDENS") == "1" {
		if err := os.WriteFile(path, actual.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, actual.Bytes()) {
		t.Fatal("runtime policy body/properties/native golden mismatch", err)
	}
}
