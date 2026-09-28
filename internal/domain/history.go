package domain

// SemanticHistoryView 是 Provider 原生历史的只读文本语义快照。
// 空历史必须使用非 nil 的空 Turns，且该视图不得用于构造 Provider 请求。
type SemanticHistoryView struct {
	Provider ProviderFamily `json:"provider"`
	Turns    []SemanticTurn `json:"turns"`
}

// SemanticTurn 表示一次已成功提交 turn 的用户与 assistant 可见文本。
type SemanticTurn struct {
	UserText      string `json:"user_text"`
	AssistantText string `json:"assistant_text"`
}
