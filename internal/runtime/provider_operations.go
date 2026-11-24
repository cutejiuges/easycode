package runtime

import "easycode/internal/provider"

// providerOperations 在 Runtime 装配时固定操作绑定，缺省能力仍在对应入口处理。
type providerOperations struct {
	inputEncoding provider.UserInputEncoder
	userInputs    provider.UserInputPreparer
	toolOutputs   provider.ToolResultPreparer
	stopEncoding  provider.StopFeedbackEncoder
	stop          provider.StopContinuationPreparer
	sessionStart  provider.SessionStartPreparer
	compaction    provider.CompactionKernel
	continuation  provider.ContinuationResetter
	planProposal  provider.PlanProposalProjector
}

func bindProviderOperations(conversation provider.Conversation) providerOperations {
	var operations providerOperations
	operations.inputEncoding, _ = conversation.(provider.UserInputEncoder)
	operations.userInputs, _ = conversation.(provider.UserInputPreparer)
	operations.toolOutputs, _ = conversation.(provider.ToolResultPreparer)
	operations.stopEncoding, _ = conversation.(provider.StopFeedbackEncoder)
	operations.stop, _ = conversation.(provider.StopContinuationPreparer)
	operations.sessionStart, _ = conversation.(provider.SessionStartPreparer)
	operations.compaction, _ = conversation.(provider.CompactionKernel)
	operations.continuation, _ = conversation.(provider.ContinuationResetter)
	operations.planProposal, _ = conversation.(provider.PlanProposalProjector)
	return operations
}
