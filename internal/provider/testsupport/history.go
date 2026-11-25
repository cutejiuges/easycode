package testsupport

import (
	"fmt"
	"runtime"
	"testing"

	"easycode/internal/domain"
)

// CheckConcurrentHistory 复用同一快照不变量，两家仍调用各自实际 projector。
func CheckConcurrentHistory(t *testing.T, appendTurn func(int), project func() domain.SemanticHistoryView) {
	t.Helper()
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-start
		for index := 0; index < 200; index++ {
			appendTurn(index)
			runtime.Gosched()
		}
	}()
	close(start)

	for {
		projection := project()
		assertCompleteProjection(t, projection)
		select {
		case <-done:
			final := project()
			assertCompleteProjection(t, final)
			if len(final.Turns) != 200 {
				t.Fatalf("final turn count: %d", len(final.Turns))
			}
			return
		default:
			runtime.Gosched()
		}
	}
}

func assertCompleteProjection(t *testing.T, projection domain.SemanticHistoryView) {
	t.Helper()
	for index, turn := range projection.Turns {
		wantUser := fmt.Sprintf("user-%d", index)
		wantAssistant := fmt.Sprintf("assistant-%d", index)
		if len(turn.Messages) == 0 || len(turn.Messages) > 2 || turn.Messages[0].Text != wantUser || (len(turn.Messages) == 2 && turn.Messages[1].Text != wantAssistant) || (len(turn.Messages) == 1 && index != len(projection.Turns)-1) {
			t.Fatalf("partial turn %d: %#v", index, turn)
		}
	}
}
