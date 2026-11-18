package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func preparedReasoning(t *testing.T, readable bool, ready ...tool.ReadyCall) *provider.PreparedSample {
	t.Helper()
	base := newPreparedSample(t, func() {})
	envelope, _ := base.Envelope()
	usage, _ := base.Usage()
	receipt, _ := base.Receipt()
	availability := domain.ReasoningUnavailable
	if readable {
		availability = domain.ReasoningReadable
	}
	presentation, err := domain.NewSamplePresentation(uint32(1+len(ready)), []domain.ReasoningBinding{{Block: 0, Item: 0, Source: domain.ReasoningOpenAISummary, Availability: availability}})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := provider.NewPreparedSample(envelope, usage, receipt, presentation, func() {}, func() {}, ready...)
	if err != nil {
		t.Fatal(err)
	}
	return sample
}
func reasoningStreamEvents(t *testing.T, complete bool) []provider.StreamEvent {
	t.Helper()
	start, _ := protocol.NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	delta, _ := protocol.NewReasoningDelta(0, domain.ReasoningOpenAISummary, 0, "allowed")
	events := []provider.StreamEvent{semanticProviderEvent(t, start), semanticProviderEvent(t, delta)}
	if complete {
		closed, _ := protocol.NewReasoningCompleted(0, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: domain.ReasoningReadable, TotalBytes: 7, Text: "allowed"})
		events = append(events, semanticProviderEvent(t, closed), completedProviderEventWithSample(t, preparedReasoning(t, true)))
	} else {
		failed, _ := provider.NewFailedStreamEvent(errors.New("fixture stream failed"))
		events = append(events, failed)
	}
	return events
}

type reasoningAdmissionJournal struct {
	fakeJournal
	admitted, release      chan struct{}
	sampleContextCancelled bool
}

func (j *reasoningAdmissionJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	for _, draft := range drafts {
		if draft.EventKind() == session.EventSampleUsage {
			close(j.admitted)
			<-j.release
			j.sampleContextCancelled = ctx.Err() != nil
			break
		}
	}
	return j.fakeJournal.AppendBatch(ctx, drafts)
}
func TestReasoningCancellationDuringAcceptedCommitWaitsForRealResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal := &reasoningAdmissionJournal{admitted: make(chan struct{}), release: make(chan struct{})}
			if fail {
				journal.failAt = 3
			}
			r := newTestRuntime(t, &fakeConversation{stream: fixedStream(reasoningStreamEvents(t, true)...)}, journal)
			type result struct {
				events []protocol.Event
				err    error
			}
			done := make(chan result, 1)
			go func() { events, err := collectTurn(r, ctx, nil); done <- result{events, err} }()
			<-journal.admitted
			cancel()
			select {
			case <-done:
				t.Fatal("cancel abandoned accepted commit owner")
			default:
			}
			close(journal.release)
			output := <-done
			if journal.sampleContextCancelled {
				t.Fatal("accepted sample inherited cancellation")
			}
			if fail {
				p, err := protocol.DecodeTurnFailed(output.events[len(output.events)-1])
				if err != nil || p.Resolution == nil || p.Resolution.Disposition != protocol.SampleUncertain {
					t.Fatal(p, err)
				}
			} else {
				if output.err != nil || output.events[len(output.events)-1].Kind != protocol.EventTurnCompleted {
					t.Fatal(output.err)
				}
				commits := 0
				for _, event := range output.events {
					if event.Kind == protocol.EventTranscriptCommitted {
						p, _ := protocol.DecodeTranscriptCommitted(event)
						if p.Origin == protocol.TranscriptCommitSample {
							commits++
						}
					}
				}
				if commits != 1 {
					t.Fatal("accepted durable commit was not emitted exactly once")
				}
			}
		})
	}
}
