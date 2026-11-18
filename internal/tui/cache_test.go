package tui

import (
	"context"

	"easycode/internal/protocol"
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
