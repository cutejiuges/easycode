package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestInferencePanelBoundedScrollAndReadOnlyKeys(t *testing.T) {
	thinking, _ := domain.NewThinkingSettings(domain.ThinkingEnabled, 8192, domain.DisplayOmitted)
	settings, _ := domain.NewAnthropicInference(thinking, "high", 16384)
	for _, size := range [][2]int{{80, 24}, {24, 10}, {3, 2}, {1, 1}} {
		model := NewModel(context.Background(), "fixture", &contextFixtureSession{}).WithInferenceSettings(settings)
		model.width, model.height = size[0], size[1]
		model.contextPanel = &protocol.ContextSnapshotPayload{Model: "hostile\x1b[31m\x00model", Revision: 3}
		original := model.inference
		all := strings.Join(model.contextPanelLines(), "\n")
		for _, field := range []string{"enabled", "8192", "omitted", "high", "16384", "explicit", "endpoint support not verified", "independent observations"} {
			if size[0] == 80 && !strings.Contains(all, field) {
				t.Fatal("startup selection missing", field)
			}
		}
		for i := 0; i < len(model.contextPanelLines())+5; i++ {
			next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
			model = next.(Model)
			if cmd != nil {
				t.Fatal("scroll performed IO")
			}
		}
		if model.contextScroll != model.contextScrollLimit() {
			t.Fatal("scroll unbounded")
		}
		for _, key := range []tea.KeyType{tea.KeyPgUp, tea.KeyHome, tea.KeyPgDown, tea.KeyEnd} {
			next, cmd := model.Update(tea.KeyMsg{Type: key})
			model = next.(Model)
			if cmd != nil {
				t.Fatal("read key performed IO")
			}
			view := model.View()
			if strings.ContainsAny(view, "\x1b\x00") || len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("unsafe or unbounded render")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("width exceeded")
				}
			}
			if size[0] >= 3 && !strings.Contains(view, "Esc") {
				t.Fatal("close operation lost")
			}
		}
		if original != model.inference {
			t.Fatal("reading modified policy")
		}
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if next.(Model).contextPanel != nil {
			t.Fatal("panel did not close")
		}
	}
}

func TestInferenceUnsentAndExplicitDefaultRemainDistinct(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		s, _ := domain.DefaultInferenceSettings(family)
		model := NewModel(context.Background(), "fixture", &contextFixtureSession{}).WithInferenceSettings(s)
		view := strings.Join(model.inferenceLines(), "\n")
		if !strings.Contains(view, "not sent (provider default)") {
			t.Fatal("missing omission semantics")
		}
		if family == domain.ProviderOpenAI && !strings.Contains(view, "server default / unknown") {
			t.Fatal("invented server cap")
		}
		if family == domain.ProviderAnthropic && !strings.Contains(view, "4096 (default)") {
			t.Fatal("default cap lost")
		}
	}
	s, _ := domain.NewAnthropicInference(domain.ThinkingSettings{}, "", 4096)
	model := NewModel(context.Background(), "fixture", &contextFixtureSession{}).WithInferenceSettings(s)
	if !strings.Contains(strings.Join(model.inferenceLines(), "\n"), "4096 (explicit)") {
		t.Fatal("source inferred from resolved value")
	}
}
