package provider

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

type nativeSequenceEntry interface {
	NativeRole() domain.NativeCommitRole
	NativeTurn() domain.TurnID
	NativeInputMetadata() *InputMetadata
	NativeSessionStartTarget() domain.SessionStartTarget
}

// NativeSequenceRules 提供时序检查所需的协议验证和原生配对。
type NativeSequenceRules[E nativeSequenceEntry, C any] struct {
	ValidateEntry func(E) error
	Calls         func(E) []C
	MatchOutputs  func([]C, E) error
	Feedback      func(E) (domain.StopFeedback, error)
}

type nativeSequence[E nativeSequenceEntry, C any] struct {
	rules           NativeSequenceRules[E, C]
	err             error
	pending         []C
	turn            domain.TurnID
	awaitingInitial domain.TurnID
	lastRole        domain.NativeCommitRole
	index           int
	nextSample      uint32
	stopCount       int
	sampleAllowed   bool
	steeringAllowed bool
	seenInputs      map[domain.InputID]struct{}
	seenTurns       map[domain.TurnID]struct{}
}

func newNativeSequence[E nativeSequenceEntry, C any](rules NativeSequenceRules[E, C]) *nativeSequence[E, C] {
	return &nativeSequence[E, C]{
		rules: rules, seenInputs: make(map[domain.InputID]struct{}), seenTurns: make(map[domain.TurnID]struct{}),
	}
}

func (s *nativeSequence[E, C]) observe(entry E) {
	if s.err != nil {
		return
	}
	s.index++
	s.err = s.advance(entry)
	if entry.NativeRole() != domain.NativeCommitSessionStartContext {
		s.lastRole = entry.NativeRole()
	}
}

func (s *nativeSequence[E, C]) advance(entry E) error {
	if err := s.rules.ValidateEntry(entry); err != nil {
		return fmt.Errorf("entry %d is invalid: %w", s.index, err)
	}
	if s.awaitingInitial != "" && entry.NativeRole() != domain.NativeCommitUserInputs {
		return fmt.Errorf("session start context requires adjacent initial user inputs")
	}
	switch entry.NativeRole() {
	case domain.NativeCommitSessionStartContext:
		return s.acceptSessionStart(entry.NativeSessionStartTarget())
	case domain.NativeCommitUserInputs:
		return s.acceptInputs(entry)
	case domain.NativeCommitSample:
		if len(s.pending) != 0 || !s.sampleAllowed || entry.NativeTurn() != s.turn {
			return fmt.Errorf("sample history position is invalid")
		}
		s.sampleAllowed, s.steeringAllowed = false, false
		s.nextSample++
		s.pending = s.rules.Calls(entry)
	case domain.NativeCommitHookFeedback:
		feedback, err := s.rules.Feedback(entry)
		if err != nil {
			return err
		}
		sample, _ := feedback.Identity.Target.AssistantSample()
		if len(s.pending) != 0 || s.sampleAllowed || s.turn != entry.NativeTurn() ||
			s.nextSample == 0 || sample.TurnID != s.turn || sample.SampleIndex+1 != s.nextSample ||
			s.nextSample >= 16 || s.lastRole != domain.NativeCommitSample || s.stopCount >= domain.MaxStopContinuations {
			return fmt.Errorf("invalid hook feedback history position")
		}
		s.stopCount++
		s.sampleAllowed, s.steeringAllowed = true, true
	case domain.NativeCommitToolOutputs:
		if len(s.pending) == 0 || entry.NativeTurn() != s.turn {
			return fmt.Errorf("tool outputs have no matching pending calls")
		}
		if err := s.rules.MatchOutputs(s.pending, entry); err != nil {
			return err
		}
		s.pending = nil
		s.sampleAllowed, s.steeringAllowed = true, true
	}
	return nil
}

func (s *nativeSequence[E, C]) acceptInputs(entry E) error {
	if len(s.pending) != 0 {
		return fmt.Errorf("user inputs precede pending tool outputs")
	}
	association, err := entry.NativeInputMetadata().Association()
	if err != nil {
		return err
	}
	if entry.NativeTurn() != association.TurnID() {
		return fmt.Errorf("input turn association mismatch")
	}
	if s.awaitingInitial != "" {
		if association.TurnID() != s.awaitingInitial || association.Placement() != domain.InputInitial {
			return fmt.Errorf("session start context does not match initial user inputs")
		}
		s.awaitingInitial = ""
	}
	if association.Placement() == domain.InputInitial {
		if _, exists := s.seenTurns[association.TurnID()]; exists {
			return fmt.Errorf("initial input is duplicated")
		}
		s.seenTurns[association.TurnID()] = struct{}{}
		s.turn, s.nextSample, s.stopCount = association.TurnID(), 0, 0
	} else if !s.steeringAllowed || s.turn != association.TurnID() || s.nextSample != association.SampleIndex() {
		return fmt.Errorf("steering input position is invalid")
	}
	for _, id := range association.IDs() {
		if _, exists := s.seenInputs[id]; exists {
			return fmt.Errorf("input ID is duplicated")
		}
		s.seenInputs[id] = struct{}{}
	}
	s.sampleAllowed, s.steeringAllowed = true, false
	return nil
}

func (s *nativeSequence[E, C]) acceptSessionStart(target domain.SessionStartTarget) error {
	if len(s.pending) != 0 {
		return fmt.Errorf("session start context precedes pending tool outputs")
	}
	if target.Source == domain.SessionStartCompact {
		scope := target.Scope
		if scope.Trigger == domain.CompactionAuto {
			if scope.SampleIndex == 0 {
				if _, alreadyStarted := s.seenTurns[scope.TurnID]; alreadyStarted {
					return fmt.Errorf("compact context has a stale initial sample")
				}
			} else if scope.TurnID != s.turn || scope.SampleIndex != s.nextSample {
				return fmt.Errorf("compact context does not match continuation")
			}
		}
		return nil
	}
	if _, exists := s.seenTurns[target.TurnID]; exists {
		return fmt.Errorf("session start context applies to an existing turn")
	}
	s.awaitingInitial = target.TurnID
	return nil
}

func (s *nativeSequence[E, C]) complete() error {
	if s.err != nil {
		return s.err
	}
	if s.awaitingInitial != "" {
		return fmt.Errorf("session start context has no initial user inputs")
	}
	return nil
}

func validateNativeSequence[E nativeSequenceEntry, C any](entries []E, rules NativeSequenceRules[E, C]) *nativeSequence[E, C] {
	sequence := newNativeSequence(rules)
	for _, entry := range entries {
		sequence.observe(entry)
		if sequence.err != nil {
			break
		}
	}
	return sequence
}

// ValidateNativeSequence 按当前顺序验证条目和工具闭合关系。
func ValidateNativeSequence[E nativeSequenceEntry, C any](entries []E, rules NativeSequenceRules[E, C]) error {
	return validateNativeSequence(entries, rules).complete()
}

// LastNativeConversationEntry 找到决定实际回合与采样资格的原生前驱。
func LastNativeConversationEntry[E nativeSequenceEntry](entries []E) (E, bool) {
	if index := lastNativeConversationIndex(entries); index >= 0 {
		return entries[index], true
	}
	var zero E
	return zero, false
}

func lastNativeConversationIndex[E nativeSequenceEntry](entries []E) int {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].NativeRole() != domain.NativeCommitSessionStartContext {
			return i
		}
	}
	return -1
}

// PendingNativeCalls 返回最后一个原生 sample 中仍待配对的调用。
func PendingNativeCalls[E nativeSequenceEntry, C any](entries []E, rules NativeSequenceRules[E, C], unavailable string) ([]C, error) {
	return validateNativeSequence(entries, rules).pendingCalls(unavailable)
}

func (s *nativeSequence[E, C]) pendingCalls(unavailable string) ([]C, error) {
	if err := s.complete(); err != nil {
		return nil, err
	}
	if s.lastRole != domain.NativeCommitSample || len(s.pending) == 0 {
		return nil, fmt.Errorf("%s", unavailable)
	}
	return s.pending, nil
}

// EncodeNativeToolOutputs 按模型发出的调用顺序编码冻结结果。
func EncodeNativeToolOutputs[C any](calls []C, results []tool.ModelOutput, mismatch string, encode func(C, tool.ModelOutput) (C, error)) ([]C, error) {
	if len(results) != len(calls) {
		return nil, fmt.Errorf("%s", mismatch)
	}
	outputs := make([]C, len(results))
	for i, result := range results {
		item, err := encode(calls[i], result)
		if err != nil {
			return nil, err
		}
		outputs[i] = item
	}
	return outputs, nil
}
