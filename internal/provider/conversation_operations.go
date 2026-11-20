package provider

import "sync"

// ConversationOperationState 表示接纳前已有的操作。流与其准备提交可同时占有会话。
type ConversationOperationState uint32

const (
	ConversationIdle    ConversationOperationState = 0
	ConversationActive  ConversationOperationState = 1 << 0
	ConversationPending ConversationOperationState = 1 << 1
)

// ConversationOperations 记录会话的流和原生提交所有权。零值可接纳操作。
type ConversationOperations struct {
	mu    sync.Mutex
	state ConversationOperationState
}

// Begin 接纳需要独占会话的操作。返回 Idle 表示调用方取得所有权。
func (operations *ConversationOperations) Begin() ConversationOperationState {
	return operations.begin(ConversationActive)
}

func (operations *ConversationOperations) begin(kind ConversationOperationState) ConversationOperationState {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	if operations.state&ConversationActive != 0 {
		return ConversationActive
	}
	if operations.state&ConversationPending != 0 {
		return ConversationPending
	}
	operations.state = kind
	return ConversationIdle
}

// Busy 报告是否正在执行独占操作，或等待原生提交收口。
func (operations *ConversationOperations) Busy() bool {
	return operations.State() != ConversationIdle
}

// State 读取活动和待提交标记的同一快照。
func (operations *ConversationOperations) State() ConversationOperationState {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return operations.state
}

// End 归还活动操作的所有权。尚未收口的提交仍阻止下一次接纳。
func (operations *ConversationOperations) End() {
	operations.mu.Lock()
	operations.state &^= ConversationActive
	operations.mu.Unlock()
}

// HoldPrepared 在已接纳流结束前登记准备提交，由其唯一 resolver 收口。
func (operations *ConversationOperations) HoldPrepared() {
	operations.mu.Lock()
	operations.state |= ConversationPending
	operations.mu.Unlock()
}

// ResolvePrepared 在 finalize 或 discard 的工作完成后归还提交所有权。
func (operations *ConversationOperations) ResolvePrepared() {
	operations.mu.Lock()
	operations.state &^= ConversationPending
	operations.mu.Unlock()
}
