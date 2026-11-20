package headless

import "easycode/internal/protocol"

type streamCommandPair struct {
	wire string
	kind protocol.CommandKind
}

// streamCommandPairs 将外部 v1 命令名与内部命令类型绑定在同一处。
func streamCommandPairs() [13]streamCommandPair {
	return [...]streamCommandPair{
		{"tasks.query", protocol.CommandQueryTasks},
		{"cache.query", protocol.CommandQueryCache},
		{"effort.query", protocol.CommandQueryEffort},
		{"effort.set", protocol.CommandSetEffort},
		{"context.query", protocol.CommandQueryContext},
		{"context.compact", protocol.CommandCompactContext},
		{"context.cancel", protocol.CommandCancelCompaction},
		{"input.append", protocol.CommandSubmitConversationalInput},
		{"input.retract", protocol.CommandRetractInputs},
		{"turn.stop", protocol.CommandStopTurn},
		{"input.submit", protocol.CommandSubmitInput},
		{"turn.interrupt", protocol.CommandInterrupt},
		{"session.shutdown", protocol.CommandShutdown},
	}
}

func streamCommandKind(wire string) protocol.CommandKind {
	for _, pair := range streamCommandPairs() {
		if pair.wire == wire {
			return pair.kind
		}
	}
	return ""
}

func streamCommandType(kind protocol.CommandKind) string {
	for _, pair := range streamCommandPairs() {
		if pair.kind == kind {
			return pair.wire
		}
	}
	return ""
}
