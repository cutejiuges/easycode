package protocol

import "easycode/internal/domain"

// CommandKind 描述宿主发送给 Runtime 的命令类型。
type CommandKind string

const (
	CommandSubmitInput CommandKind = "submit_input"
	CommandInterrupt   CommandKind = "interrupt"
	CommandShutdown    CommandKind = "shutdown"
)

// Command 是进入 Runtime 的版本化命令信封。
type Command struct {
	Version   int              `json:"version"`
	Kind      CommandKind      `json:"kind"`
	SessionID domain.SessionID `json:"session_id,omitempty"`
	ThreadID  domain.ThreadID  `json:"thread_id,omitempty"`
	Text      string           `json:"text,omitempty"`
}
