package protocol

import (
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestCompactionTrustAndPresentationUseExactScope(t *testing.T) {
	id := domain.CompactionID("01900000-0000-7000-8000-000000000020")
	auto, err := domain.NewAutoCompactionScope(testTurnID, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []domain.CompactionScope{domain.ManualCompactionScope(), auto} {
		t.Run(string(scope.Trigger), func(t *testing.T) {
			target, err := domain.NewPreCompactHookTarget(id, scope)
			if err != nil {
				t.Fatal(err)
			}
			matcher, _ := domain.ParsePreCompactHookScope(string(scope.Trigger))
			command := strings.Repeat("\x01", domain.MaxHookCommandBytes)
			declaration, err := domain.NewPreCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, strings.Repeat("a", 64), "verify", matcher, command, 5)
			if err != nil {
				t.Fatal(err)
			}
			request := HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000024", Target: target, PlanDigest: strings.Repeat("b", 64), Declaration: declaration}
			event, err := NewHookTrustRequested(request)
			if err != nil || len(event.Payload) <= 16<<10 {
				t.Fatalf("large review not encoded: %v, %d bytes", err, len(event.Payload))
			}
			event.SessionID, event.ThreadID = testSessionID, testThreadID
			correlated, err := NewCorrelatedContextItem("compact-1", event)
			if err != nil {
				t.Fatal(err)
			}
			fromItem, _ := correlated.ContextEvent()
			if got, err := DecodeHookTrustRequested(fromItem); err != nil || got != request {
				t.Fatal("review projection changed command/identity", err)
			}
			if _, err := NewCorrelatedTurnItem("compact-1", event); err == nil {
				t.Fatal("compaction review admitted as ordinary turn")
			}
			response, err := NewHookTrustResponse(testSessionID, testThreadID, scope.TurnID, request, HookTrustSession)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := codec.MarshalStable(response)
			if err != nil {
				t.Fatal(err)
			}
			if cmd, err := DecodeResolveHookTrustCommand("trust-1", encoded); err != nil || cmd.HookTrustResponse() != response {
				t.Fatal("scoped response could not reach current owner", err)
			}
			resolved, err := NewHookTrustResolved(response)
			if err != nil || resolved.Validate() != nil {
				t.Fatal("scoped resolution was invalid", err)
			}
			forged := resolved
			forged.SessionID = domain.SessionID(testThreadID)
			if _, err := NewCorrelatedContextItem("compact-1", forged); err == nil {
				t.Fatal("resolution accepted a different session")
			}
			event.CompactionID = domain.CompactionID(testThreadID)
			if _, err := NewCorrelatedContextItem("compact-1", event); err == nil {
				t.Fatal("review accepted different compaction")
			}
			active, err := NewCompactionSummarizing(testSessionID, testThreadID, id, scope)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := CompactionEventScope(active); err != nil || got != scope || !IsCompactionPresentation(active) {
				t.Fatal("summary phase lost real scope", err)
			}
			active.Payload = []byte(`{"scope":{"trigger":"manual","sample_index":0},"focus":"leaked"}`)
			if _, err := NewCorrelatedContextItem("compact-1", active); err == nil {
				t.Fatal("summary phase accepted unknown content")
			}
			post := PostCompactInstalledPayload{Scope: scope, CheckpointSequence: 3, Revision: 2, PlanDigest: request.PlanDigest, Count: 1}
			installed, err := NewPostCompactInstalled(testSessionID, testThreadID, id, post)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := DecodePostCompactInstalled(installed); err != nil || got != post || !IsCompactionPresentation(installed) {
				t.Fatal("installed checkpoint lost post plan scope", err)
			}
			installed.Payload = append(installed.Payload[:len(installed.Payload)-1], []byte(`,"compact_summary":"private"}`)...)
			if _, err := NewCorrelatedContextItem("compact-1", installed); err == nil {
				t.Fatal("post-compaction installation accepted private summary")
			}
			closed := PostCompactClosedPayload{Scope: scope, CheckpointSequence: 3, PlanDigest: request.PlanDigest,
				State: PostCompactSucceeded, Code: domain.HookOK, Finished: 1}
			if _, err := NewPostCompactClosed(testSessionID, testThreadID, id, closed); err == nil {
				t.Fatal("post check success accepted without a durable close")
			}
		})
	}
}
