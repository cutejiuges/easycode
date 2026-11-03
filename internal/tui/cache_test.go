package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type cacheFixtureSession struct {
	contextFixtureSession
	queries []protocol.RequestID
}

func (s *cacheFixtureSession) QueryCache(_ context.Context, id protocol.RequestID) (protocol.CommandResult, error) {
	s.queries = append(s.queries, id)
	command, _ := protocol.NewQueryCacheCommand(id)
	result, err := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
	item, _ := protocol.NewCommandResultItem(result)
	s.outputs <- item
	return result, err
}
func cachePanelFixture(t *testing.T) protocol.CacheSnapshotPayload {
	t.Helper()
	data, err := os.ReadFile("../protocol/testdata/cache_snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var p protocol.CacheSnapshotPayload
	if err = codec.UnmarshalRecord(data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCacheLocalCommandAndLateResponseIsolation(t *testing.T) {
	session := &cacheFixtureSession{}
	model := NewModel(context.Background(), "test", session)
	for _, text := range []string{"/cache args", "/cache\nargs"} {
		_ = model.composer.replace(0, len(model.composer.text), text)
		before := model.composer
		next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = next.(Model)
		if cmd != nil || !reflect.DeepEqual(before, model.composer) || len(session.queries) != 0 {
			t.Fatal("rejected cache query lost draft or started work")
		}
	}
	_ = model.composer.replace(0, len(model.composer.text), "/cache")
	next, submit := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	first := model.queryRequestID
	if submit == nil || first == "" {
		t.Fatal("cache query not prepared")
	}
	submit()
	oldAck := <-session.outputs
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	_ = model.composer.replace(0, len(model.composer.text), "/cache")
	next, submit = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	second := model.queryRequestID
	if first == second {
		t.Fatal("query identity reused")
	}
	submit()
	before := model.composer
	next, _ = model.projectControlItem(oldAck)
	model = next.(Model)
	if !reflect.DeepEqual(before, model.composer) || model.pending == nil {
		t.Fatal("late ack modified new query")
	}
	next, _ = model.projectControlItem(<-session.outputs)
	model = next.(Model)
	sid := domain.SessionID("00000000-0010-7000-8000-000000000010")
	tid := domain.ThreadID("00000000-0011-7000-8000-000000000011")
	event, err := protocol.NewCacheSnapshot(sid, tid, cachePanelFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := protocol.NewCorrelatedContextItem(first, event)
	next, _ = model.projectControlItem(old)
	model = next.(Model)
	if model.cachePanel != nil {
		t.Fatal("stale panel reopened")
	}
	current, _ := protocol.NewCorrelatedContextItem(second, event)
	next, _ = model.projectControlItem(current)
	model = next.(Model)
	if model.cachePanel == nil {
		t.Fatal("current panel not opened")
	}
	if len(model.inputs) != 0 || len(model.transcript) != 0 || len(session.submits) != 0 || len(model.composer.history.entries) != 0 {
		t.Fatal("query became conversation input or recall")
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	_ = model.composer.replace(0, 0, "new draft")
	draft := model.composer
	next, _ = model.projectControlItem(current)
	model = next.(Model)
	if model.cachePanel != nil || !reflect.DeepEqual(draft, model.composer) {
		t.Fatal("closed panel response changed draft")
	}
	next, _ = model.Update(cacheQueryFailedMsg{generation: 1, err: context.Canceled})
	model = next.(Model)
	if !reflect.DeepEqual(draft, model.composer) {
		t.Fatal("stale error changed draft")
	}
	for _, text := range []string{"/cacheable", "/tmp/cache", "ordinary /cache"} {
		if _, local, _ := parseContextCommand(text); local {
			t.Fatal("ordinary slash intercepted")
		}
	}
}
func cacheSnapshotModel(t *testing.T, name string, width, height int) Model {
	t.Helper()
	p := cachePanelFixture(t)
	if name == "cache_restored" {
		p.Baseline = "restored_baseline"
		p.ChangeReason = "restored_baseline"
	}
	if strings.HasPrefix(name, "cache_unknown") || name == "cache_observed" || name == "cache_zero" {
		metric := domain.UnknownUsageMetric()
		input, read := metric, metric
		var ratio *float64
		observed := false
		if name == "cache_observed" || name == "cache_zero" {
			input = domain.KnownUsageMetric(30)
			read = domain.KnownUsageMetric(70)
			v := .7
			if name == "cache_zero" {
				read = domain.KnownUsageMetric(0)
				v = 0
			}
			ratio = &v
			observed = name != "cache_zero"
		}
		usage, _ := domain.NewSampleUsage(input, read, domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(5), metric)
		if strings.HasPrefix(name, "cache_unknown") {
			p.Family = domain.ProviderAnthropic
			p.Wire = "messages"
			usage, _ = domain.NewSampleUsage(input, read, metric, domain.KnownUsageMetric(5), domain.NotApplicableUsageMetric())
		}
		payload, _ := protocol.NewUsagePayload(usage)
		p.LastSample = &protocol.CacheObservationPayload{Purpose: domain.RequestSample, Usage: payload, ReadRatio: ratio, ReadObserved: observed, RequestFingerprint: domain.ContentFingerprint([]byte("request")), NativeFingerprint: domain.ContentFingerprint([]byte("native")), Transport: domain.TransportHTTP, Mode: domain.RequestFull, Relation: domain.NativeFirst, Reason: domain.ResetFirst, Fallback: domain.FallbackNone}
		p.LastTurn = &payload
	}
	model := NewModel(context.Background(), "test", &cacheFixtureSession{})
	model.width, model.height = width, height
	model.cachePanel = &p
	return model
}
func TestCachePanelSnapshotsAndNoColor(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
	}{{"cache", 80, 36}, {"cache_narrow", 24, 18}, {"cache_tiny", 1, 1}, {"cache_restored", 80, 36}, {"cache_unknown", 80, 36}, {"cache_observed", 80, 36}, {"cache_zero", 80, 36}, {"cache_unknown_narrow", 24, 18}} {
		t.Run(test.name, func(t *testing.T) {
			model := cacheSnapshotModel(t, test.name, test.width, test.height)
			before := model.composer
			got := model.View()
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Fatalf("cache snapshot differs:\n%s", got)
			}
			if test.name == "cache_unknown_narrow" && (!strings.Contains(got, "unknown") || !strings.Contains(got, "Configured") || !strings.Contains(got, "Esc")) {
				t.Fatal("narrow panel hid uncertainty or controls")
			}
			if strings.Contains(got, "\x1b") || !reflect.DeepEqual(before, model.composer) || len(strings.Split(got, "\n")) > test.height {
				t.Fatal("render side effect or bounds violation")
			}
			for _, line := range strings.Split(got, "\n") {
				if ansi.StringWidth(line) > test.width {
					t.Fatal("line exceeds width")
				}
			}
		})
	}
	model := cacheSnapshotModel(t, "cache", 40, 10)
	model.cachePanel.Model = "bad\x1b[2J\rpath"
	if strings.Contains(model.View(), "\x1b") {
		t.Fatal("hostile model reached terminal")
	}
	before := model.cachePanel.ContentFingerprint
	next, _ := model.Update(tea.WindowSizeMsg{Width: 19, Height: 4})
	if next.(Model).cachePanel.ContentFingerprint != before {
		t.Fatal("resize changed content fingerprint")
	}
}

func TestCachePanelInterruptPreservesDraftAndTargetsActiveTurn(t *testing.T) {
	session := &cacheFixtureSession{}
	model := NewModel(context.Background(), "test", session)
	payload := cachePanelFixture(t)
	model.cachePanel = &payload
	model.activeTurnID = "01900000-0000-7000-8000-000000000001"
	model.state = stateStreaming
	model.queryRequestID = "cache-active"
	model.queryKind = protocol.CommandQueryCache
	_ = model.composer.replace(0, 0, "unfinished draft")
	draft := model.composer
	next, interrupt := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)
	if model.cachePanel != nil || model.queryRequestID != "" || interrupt == nil || !reflect.DeepEqual(draft, model.composer) {
		t.Fatal("panel consumed interrupt or changed draft")
	}
	interrupt()
	if session.interrupts != 1 || len(session.queries) != 0 {
		t.Fatal("interrupt routed to a new query")
	}
}
