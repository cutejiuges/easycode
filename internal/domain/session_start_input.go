package domain

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

type sessionStartHookInput struct {
	source SessionStartSource
	id     CompactionID
	scope  CompactionScope
}

type sessionStartInputWire struct {
	Event   HookEvent          `json:"hook_event_name"`
	Source  SessionStartSource `json:"source"`
	Session SessionID          `json:"session_id"`
	Thread  ThreadID           `json:"thread_id"`
	CWD     string             `json:"cwd"`
	Turn    TurnID             `json:"turn_id,omitempty"`
	ID      CompactionID       `json:"compaction_id,omitempty"`
	Trigger CompactionTrigger  `json:"trigger,omitempty"`
	Index   *uint32            `json:"sample_index,omitempty"`
}

// NewSessionStartHookInput 仅把已验证目标的会话与触发元数据交给脚本。
func NewSessionStartHookInput(session SessionID, thread ThreadID, target SessionStartTarget, cwd string) (HookInput, error) {
	if err := target.Validate(); err != nil {
		return HookInput{}, err
	}
	v := HookInput{
		Event: HookSessionStart, SessionID: session, ThreadID: thread, CWD: cwd, TurnID: target.turn(),
		sessionStart: &sessionStartHookInput{source: target.Source, id: target.CompactionID, scope: target.Scope},
	}
	return v, v.Validate()
}

func (v HookInput) sessionStartBytes() ([]byte, error) {
	input := v.sessionStart
	if input == nil || v.stop != nil || v.compaction != nil || v.promptSubmit != nil || v.hasToolFields() ||
		!v.SessionID.Valid() || !v.ThreadID.Valid() || !input.source.Valid() || !filepath.IsAbs(v.CWD) ||
		filepath.Clean(v.CWD) != v.CWD || !utf8.ValidString(v.CWD) || strings.ContainsRune(v.CWD, 0) {
		return nil, HookConfigInvalid
	}
	wire := sessionStartInputWire{
		Event: v.Event, Source: input.source, Session: v.SessionID, Thread: v.ThreadID, CWD: v.CWD, Turn: v.TurnID,
	}
	if input.source == SessionStartCompact {
		if !input.id.Valid() || input.scope.Validate() != nil || input.scope.TurnID != v.TurnID {
			return nil, HookConfigInvalid
		}
		wire.ID, wire.Trigger = input.id, input.scope.Trigger
		if input.scope.Trigger == CompactionAuto {
			wire.Index = &input.scope.SampleIndex
		}
	} else if !v.TurnID.Valid() || input.id != "" || input.scope != (CompactionScope{}) {
		return nil, HookConfigInvalid
	}
	data, err := codec.MarshalStable(wire)
	if err != nil || len(data) > MaxHookInputBytes {
		return nil, HookInputLimit
	}
	return data, nil
}

func decodeSessionStartHookInput(data []byte, fields map[string]json.RawMessage) (HookInput, error) {
	var w sessionStartInputWire
	if codec.UnmarshalRecord(data, &w) != nil || w.Event != HookSessionStart {
		return HookInput{}, HookConfigInvalid
	}
	input := sessionStartHookInput{source: w.Source, id: w.ID}
	_, hasTurn := fields["turn_id"]
	_, hasID := fields["compaction_id"]
	_, hasTrigger := fields["trigger"]
	_, hasIndex := fields["sample_index"]
	if w.Source == SessionStartCompact {
		auto := w.Trigger == CompactionAuto
		if !hasID || !hasTrigger || hasTurn != auto || hasIndex != auto {
			return HookInput{}, HookConfigInvalid
		}
		input.scope = CompactionScope{Trigger: w.Trigger, TurnID: w.Turn}
		if auto {
			input.scope.SampleIndex = *w.Index
		}
	} else if !hasTurn || hasID || hasTrigger || hasIndex {
		return HookInput{}, HookConfigInvalid
	}
	v := HookInput{Event: w.Event, SessionID: w.Session, ThreadID: w.Thread, CWD: w.CWD, TurnID: w.Turn, sessionStart: &input}
	return v, v.Validate()
}
