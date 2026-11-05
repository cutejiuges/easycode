package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type transcriptAnchor struct {
	key    markdownViewKey
	source int
}
type transcriptViewport struct {
	lastKey    markdownViewKey
	lastLength int
	newContent bool
	anchors    []transcriptAnchor
	anchor     transcriptAnchor
	scroll     int
	detached   bool
	notice     string
}

func (model Model) transcriptHeight() int {
	status := 0
	if model.reading.detached || model.reading.notice != "" {
		status++
	}
	if model.compactionStatus != "" {
		status += strings.Count(ansi.Hardwrap(model.compactionStatus, model.widthOrDefault(), true), "\n") + 1
	}
	return max(1, model.heightOrDefault()-strings.Count(model.composer.view(true), "\n")-strings.Count(model.renderInputs(), "\n")-5-status)
}
func (model Model) navigateTranscript(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	model.main.resetHydration()
	view := &model.reading
	height := model.transcriptHeight()
	bottom := max(0, len(model.markdown.lines)-height)
	if !view.detached {
		view.scroll = bottom
	}
	if key.Type == tea.KeyPgUp {
		view.scroll = max(0, view.scroll-height)
		view.detached = true
	} else {
		view.scroll = min(bottom, view.scroll+height)
		view.detached = view.scroll < bottom
	}
	view.notice = ""
	if !view.detached {
		view.newContent = false
	}
	if view.scroll < len(view.anchors) {
		view.anchor = view.anchors[view.scroll]
	}
	return model, nil
}
func (view *transcriptViewport) restore(anchors []transcriptAnchor) {
	view.anchors = anchors
	if !view.detached {
		return
	}
	found := -1
	nearest := -1
	for i, anchor := range anchors {
		if anchor.key == view.anchor.key && nearest < 0 {
			nearest = i
		}
		if anchor.key == view.anchor.key && anchor.source <= view.anchor.source && (found < 0 || anchor.source > anchors[found].source) {
			found = i
		}
	}
	if found >= 0 {
		view.scroll = found
		return
	}
	if nearest >= 0 {
		view.scroll = nearest
		view.anchor = anchors[nearest]
		view.notice = "Original source position is no longer available"
		return
	}
	view.scroll = 0
	view.notice = "Earlier position left this window · Ctrl+O for history"
	if len(anchors) > 0 {
		view.anchor = anchors[0]
	}
}
