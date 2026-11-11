package anthropic

import (
	"easycode/internal/domain"
	"testing"
)

func testInference(t *testing.T, output uint64) domain.InferenceSettings {
	t.Helper()
	s, err := domain.NewAnthropicInference(domain.ThinkingSettings{}, "", output)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testWrongFamilyInference(t *testing.T) domain.InferenceSettings {
	t.Helper()
	s, err := domain.DefaultInferenceSettings(domain.ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
