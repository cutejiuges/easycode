package context

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

const (
	CurrentContextPlanVersion = 3
	providerProfileRevision   = "provider-profile-v1"
	currentInputRevision      = "current-input-v2"
)

// SourceKind 标识当前上下文计划中的来源类型。
type SourceKind string

const (
	SourceProviderProfile     SourceKind = "provider_profile"
	SourceToolCatalog         SourceKind = "tool_catalog"
	SourceProjectInstructions SourceKind = "project_instructions"
	SourceCommittedHistory    SourceKind = "committed_history"
	SourceCurrentInput        SourceKind = "current_input"
)

// SourceLifecycle 描述来源在后续计划中的更新方式。
type SourceLifecycle string

const (
	SourceLifecycleReplace SourceLifecycle = "replace"
	SourceLifecycleAppend  SourceLifecycle = "append"
)

// BudgetDecisionState 描述显式上下文预算的判定结果。
type BudgetDecisionState string

const (
	BudgetNotEnforced   BudgetDecisionState = "not_enforced"
	BudgetWithinLimit   BudgetDecisionState = "within_limit"
	BudgetOverLimit     BudgetDecisionState = "over_limit"
	BudgetIndeterminate BudgetDecisionState = "indeterminate"
)

// Budget 保存可选的显式上下文窗口及预留量。
type Budget struct {
	enabled        bool
	window         uint64
	reservedOutput uint64
	safetyMargin   uint64
}

// DisabledBudget 创建不执行硬性上限判断的预算。
func DisabledBudget() Budget { return Budget{} }

// NewBudget 创建启用的显式上下文预算。
func NewBudget(window, reservedOutput, safetyMargin uint64) (Budget, error) {
	budget := Budget{enabled: true, window: window, reservedOutput: reservedOutput, safetyMargin: safetyMargin}
	if err := budget.Validate(); err != nil {
		return Budget{}, err
	}
	return budget, nil
}

// Enabled 判断是否配置了上下文窗口。
func (budget Budget) Enabled() bool { return budget.enabled }

// Window 返回配置的完整上下文窗口。
func (budget Budget) Window() uint64 { return budget.window }

// ReservedOutput 返回输出预留 token。
func (budget Budget) ReservedOutput() uint64 { return budget.reservedOutput }

// SafetyMargin 返回安全余量 token。
func (budget Budget) SafetyMargin() uint64 { return budget.safetyMargin }

// EffectiveInputLimit 返回输入可使用的 token 上限；未启用时返回 false。
func (budget Budget) EffectiveInputLimit() (uint64, bool) {
	if !budget.enabled || budget.Validate() != nil {
		return 0, false
	}
	return budget.window - budget.reservedOutput - budget.safetyMargin, true
}

// Validate 校验预算组合且拒绝任何可能溢出的预留量。
func (budget Budget) Validate() error {
	if !budget.enabled {
		if budget.window != 0 || budget.reservedOutput != 0 || budget.safetyMargin != 0 {
			return fmt.Errorf("disabled context budget contains values")
		}
		return nil
	}
	if budget.window == 0 {
		return fmt.Errorf("context window tokens must be positive")
	}
	if budget.reservedOutput > math.MaxUint64-budget.safetyMargin {
		return fmt.Errorf("context budget reserves overflow")
	}
	if budget.reservedOutput+budget.safetyMargin >= budget.window {
		return fmt.Errorf("context budget reserves must be less than the window")
	}
	return nil
}

// ProviderProfile 保存会影响稳定上下文身份的非敏感模型信息。
type ProviderProfile struct {
	family domain.ProviderFamily
	model  string
}

// CurrentInputKind 区分首个用户 sample 与工具结果后的继续采样。
type CurrentInputKind string

const (
	CurrentInputUserText         CurrentInputKind = "user_text"
	CurrentInputToolContinuation CurrentInputKind = "tool_continuation"
)

// CurrentInput 是上下文计划使用的封闭当前输入值。
type CurrentInput struct {
	kind CurrentInputKind
	text string
}

// NewUserTextInput 创建首个 sample 的用户文本输入。
func NewUserTextInput(text string) CurrentInput {
	return CurrentInput{kind: CurrentInputUserText, text: text}
}

// NewToolContinuationInput 创建不附加共享用户文本的工具继续采样输入。
func NewToolContinuationInput() CurrentInput {
	return CurrentInput{kind: CurrentInputToolContinuation}
}

// Kind 返回 current input variant。
func (input CurrentInput) Kind() CurrentInputKind { return input.kind }

// Text 返回用户输入；tool continuation 固定为空。
func (input CurrentInput) Text() string { return input.text }

// Validate 拒绝未知 variant 和带伪造文本的 continuation。
func (input CurrentInput) Validate() error {
	switch input.kind {
	case CurrentInputUserText:
		return nil
	case CurrentInputToolContinuation:
		if input.text != "" {
			return fmt.Errorf("tool continuation must not contain user text")
		}
		return nil
	default:
		return fmt.Errorf("current input kind is invalid")
	}
}

// NewProviderProfile 创建不包含连接凭据的 Provider profile。
func NewProviderProfile(family domain.ProviderFamily, model string) (ProviderProfile, error) {
	profile := ProviderProfile{family: family, model: strings.TrimSpace(model)}
	if err := profile.Validate(); err != nil {
		return ProviderProfile{}, err
	}
	return profile, nil
}

// Family 返回 Provider 家族。
func (profile ProviderProfile) Family() domain.ProviderFamily { return profile.family }

// Model 返回模型名。
func (profile ProviderProfile) Model() string { return profile.model }

// Validate 校验 profile 的非敏感稳定字段。
func (profile ProviderProfile) Validate() error {
	if !profile.family.Valid() {
		return fmt.Errorf("provider profile family is invalid")
	}
	if strings.TrimSpace(profile.model) == "" {
		return fmt.Errorf("provider profile model is required")
	}
	return nil
}

// PlanningInput 是纯内存 planner 所需的完整强类型输入快照。
type PlanningInput struct {
	profile             ProviderProfile
	toolCatalog         tool.CatalogSnapshot
	projectInstructions domain.ProjectInstructionsSnapshot
	history             domain.SemanticHistoryView
	footprint           domain.NativeHistoryFootprint
	currentInput        CurrentInput
	budget              Budget
}

// NewPlanningInput 校验并深拷贝上下文规划输入。
func NewPlanningInput(
	profile ProviderProfile,
	toolCatalog tool.CatalogSnapshot,
	projectInstructions domain.ProjectInstructionsSnapshot,
	history domain.SemanticHistoryView,
	footprint domain.NativeHistoryFootprint,
	currentInput CurrentInput,
	budget Budget,
) (PlanningInput, error) {
	input := PlanningInput{
		profile: profile, toolCatalog: toolCatalog.Clone(), projectInstructions: projectInstructions, history: cloneSemanticHistory(history), footprint: footprint,
		currentInput: currentInput, budget: budget,
	}
	cloned, err := projectInstructions.Clone()
	if err != nil {
		return PlanningInput{}, fmt.Errorf("project instructions are invalid: %w", err)
	}
	input.projectInstructions = cloned
	if err := input.Validate(); err != nil {
		return PlanningInput{}, err
	}
	return input, nil
}

// Validate 校验来源之间的 Provider 和估算方法不变量。
func (input PlanningInput) Validate() error {
	if err := input.profile.Validate(); err != nil {
		return err
	}
	if err := input.toolCatalog.Validate(); err != nil {
		return fmt.Errorf("tool catalog is invalid: %w", err)
	}
	if _, err := input.toolCatalog.View(input.profile.family); err != nil {
		return fmt.Errorf("tool catalog Provider view is invalid: %w", err)
	}
	if err := input.currentInput.Validate(); err != nil {
		return err
	}
	if err := input.projectInstructions.Validate(); err != nil {
		return fmt.Errorf("project instructions are invalid: %w", err)
	}
	if !input.history.Provider.Valid() || input.history.Provider != input.profile.family {
		return fmt.Errorf("semantic history provider family does not match profile")
	}
	if input.history.Turns == nil {
		return fmt.Errorf("semantic history turns must not be nil")
	}
	if err := input.footprint.Validate(); err != nil {
		return err
	}
	if input.footprint.Family() != input.profile.family {
		return fmt.Errorf("native history footprint provider family does not match profile")
	}
	if input.footprint.Estimate().Method() != estimate.MethodByteHeuristic {
		return fmt.Errorf("native history footprint estimator method is unsupported")
	}
	if err := input.budget.Validate(); err != nil {
		return err
	}
	return nil
}

// PlannedSource 保存一个来源的有序元数据和估算。
type PlannedSource struct {
	kind      SourceKind
	lifecycle SourceLifecycle
	revision  string
	estimate  domain.TokenEstimate
}

// Kind 返回来源类型。
func (source PlannedSource) Kind() SourceKind { return source.kind }

// Lifecycle 返回来源更新方式。
func (source PlannedSource) Lifecycle() SourceLifecycle { return source.lifecycle }

// Revision 返回来源 schema 或已提交历史 revision。
func (source PlannedSource) Revision() string { return source.revision }

// Estimate 返回来源 token 估算。
func (source PlannedSource) Estimate() domain.TokenEstimate { return source.estimate }

func (source PlannedSource) validate() error {
	if !knownSourceKind(source.kind) || !knownSourceLifecycle(source.lifecycle) {
		return fmt.Errorf("planned context source metadata is invalid")
	}
	if strings.TrimSpace(source.revision) == "" {
		return fmt.Errorf("planned context source revision is required")
	}
	return source.estimate.Validate()
}

// BudgetDecision 保存不含 prompt 内容的预算判定。
type BudgetDecision struct {
	state          BudgetDecisionState
	effectiveLimit uint64
}

// State 返回预算判定状态。
func (decision BudgetDecision) State() BudgetDecisionState { return decision.state }

// EffectiveLimit 返回启用预算时的有效输入上限。
func (decision BudgetDecision) EffectiveLimit() (uint64, bool) {
	switch decision.state {
	case BudgetWithinLimit, BudgetOverLimit, BudgetIndeterminate:
		return decision.effectiveLimit, true
	default:
		return 0, false
	}
}

// ContextPlan 保存一次请求前的不可变上下文规划结果。
type ContextPlan struct {
	version   int
	sources   []PlannedSource
	cachePlan Plan
	total     domain.TokenEstimate
	decision  BudgetDecision
}

// Version 返回上下文计划版本。
func (plan ContextPlan) Version() int { return plan.version }

// Sources 返回有序来源的独立切片。
func (plan ContextPlan) Sources() []PlannedSource {
	return append([]PlannedSource(nil), plan.sources...)
}

// CachePlan 返回不共享分段切片的缓存计划。
func (plan ContextPlan) CachePlan() Plan {
	return Plan{version: plan.cachePlan.version, segments: cloneSegments(plan.cachePlan.segments)}
}

// TotalEstimate 返回全部来源合并后的估算。
func (plan ContextPlan) TotalEstimate() domain.TokenEstimate { return plan.total }

// Decision 返回预算判定。
func (plan ContextPlan) Decision() BudgetDecision { return plan.decision }

// Validate 校验计划版本、来源顺序、cache plan 和判定状态。
func (plan ContextPlan) Validate() error {
	if plan.version != CurrentContextPlanVersion {
		return fmt.Errorf("unsupported context plan version %d", plan.version)
	}
	var wantKinds []SourceKind
	var wantLifecycles []SourceLifecycle
	switch len(plan.sources) {
	case 4:
		wantKinds = []SourceKind{SourceProviderProfile, SourceToolCatalog, SourceCommittedHistory, SourceCurrentInput}
		wantLifecycles = []SourceLifecycle{SourceLifecycleReplace, SourceLifecycleReplace, SourceLifecycleAppend, SourceLifecycleReplace}
	case 5:
		wantKinds = []SourceKind{SourceProviderProfile, SourceToolCatalog, SourceProjectInstructions, SourceCommittedHistory, SourceCurrentInput}
		wantLifecycles = []SourceLifecycle{SourceLifecycleReplace, SourceLifecycleReplace, SourceLifecycleReplace, SourceLifecycleAppend, SourceLifecycleReplace}
	default:
		return fmt.Errorf("context plan source count is invalid")
	}
	seen := make(map[SourceKind]struct{}, len(plan.sources))
	for index, source := range plan.sources {
		if err := source.validate(); err != nil {
			return err
		}
		if source.kind != wantKinds[index] || source.lifecycle != wantLifecycles[index] {
			return fmt.Errorf("context plan source order is invalid")
		}
		if _, exists := seen[source.kind]; exists {
			return fmt.Errorf("duplicate context source kind %q", source.kind)
		}
		seen[source.kind] = struct{}{}
	}
	if err := plan.cachePlan.Validate(); err != nil {
		return fmt.Errorf("context plan cache snapshot is invalid: %w", err)
	}
	if err := plan.total.Validate(); err != nil {
		return fmt.Errorf("context plan total estimate is invalid: %w", err)
	}
	switch plan.decision.state {
	case BudgetNotEnforced:
		if plan.decision.effectiveLimit != 0 {
			return fmt.Errorf("unenforced budget contains an effective limit")
		}
	case BudgetWithinLimit, BudgetOverLimit, BudgetIndeterminate:
		if plan.decision.effectiveLimit == 0 {
			return fmt.Errorf("enforced budget effective limit is invalid")
		}
	default:
		return fmt.Errorf("context budget decision state is invalid")
	}
	return nil
}

// Planner 以纯内存方式构造确定性上下文计划。
type Planner struct{}

// NewPlanner 创建无状态上下文 planner。
func NewPlanner() *Planner { return &Planner{} }

// Plan 按固定来源顺序构造不可变上下文计划。
func (*Planner) Plan(input PlanningInput) (ContextPlan, error) {
	if err := input.Validate(); err != nil {
		return ContextPlan{}, err
	}

	profileEstimate := mustEstimated(0)
	toolView, err := input.toolCatalog.View(input.profile.family)
	if err != nil {
		return ContextPlan{}, err
	}
	toolEstimate := estimate.String(string(toolView.CanonicalJSON()))
	visibleEstimate := estimateSemanticHistory(input.history)
	historyEstimate, err := coverEstimates(visibleEstimate, input.footprint.Estimate())
	if err != nil {
		return ContextPlan{}, err
	}
	currentEstimate := mustEstimated(0)
	if input.currentInput.kind == CurrentInputUserText {
		currentEstimate = estimate.String(input.currentInput.text)
	}
	projectEstimate := mustEstimated(0)
	if input.projectInstructions.HasDocuments() {
		projectEstimate = estimate.String(input.projectInstructions.RenderedText())
	}
	totalEstimate := sumEstimates(profileEstimate, toolEstimate, projectEstimate, historyEstimate, currentEstimate)

	sources := []PlannedSource{
		{kind: SourceProviderProfile, lifecycle: SourceLifecycleReplace, revision: providerProfileRevision, estimate: profileEstimate},
		{kind: SourceToolCatalog, lifecycle: SourceLifecycleReplace, revision: toolView.Fingerprint(), estimate: toolEstimate},
	}
	if input.projectInstructions.HasDocuments() {
		sources = append(sources, PlannedSource{
			kind: SourceProjectInstructions, lifecycle: SourceLifecycleReplace,
			revision: input.projectInstructions.Revision(), estimate: projectEstimate,
		})
	}
	sources = append(sources,
		PlannedSource{kind: SourceCommittedHistory, lifecycle: SourceLifecycleAppend, revision: strconv.FormatUint(input.footprint.Revision(), 10), estimate: historyEstimate},
		PlannedSource{kind: SourceCurrentInput, lifecycle: SourceLifecycleReplace, revision: currentInputRevision, estimate: currentEstimate},
	)
	cachePlan, err := buildCachePlan(input)
	if err != nil {
		return ContextPlan{}, err
	}
	decision := decideBudget(input.budget, totalEstimate)
	plan := ContextPlan{
		version: CurrentContextPlanVersion, sources: append([]PlannedSource(nil), sources...),
		cachePlan: cachePlan, total: totalEstimate, decision: decision,
	}
	if err := plan.Validate(); err != nil {
		return ContextPlan{}, err
	}
	return plan, nil
}

func buildCachePlan(input PlanningInput) (Plan, error) {
	profileJSON, err := codec.MarshalCanonical(struct {
		Family         domain.ProviderFamily `json:"family"`
		Model          string                `json:"model"`
		SchemaRevision string                `json:"schema_revision"`
	}{input.profile.family, input.profile.model, providerProfileRevision}, MaxSegmentBytes)
	if err != nil {
		return Plan{}, err
	}
	toolView, err := input.toolCatalog.View(input.profile.family)
	if err != nil {
		return Plan{}, err
	}
	toolJSON, err := codec.MarshalCanonical(struct {
		CatalogRevision string          `json:"catalog_revision"`
		Facade          json.RawMessage `json:"facade"`
		Fingerprint     string          `json:"fingerprint"`
	}{
		CatalogRevision: input.toolCatalog.Revision(), Facade: json.RawMessage(toolView.CanonicalJSON()), Fingerprint: toolView.Fingerprint(),
	}, MaxSegmentBytes)
	if err != nil {
		return Plan{}, err
	}
	historyJSON, err := codec.MarshalCanonical(struct {
		History   domain.SemanticHistoryView `json:"history"`
		Revision  uint64                     `json:"native_revision"`
		Method    string                     `json:"estimate_method"`
		State     domain.TokenEstimateState  `json:"estimate_state"`
		Tokens    uint64                     `json:"estimated_tokens"`
		HasTokens bool                       `json:"has_estimated_tokens"`
	}{
		History: cloneSemanticHistory(input.history), Revision: input.footprint.Revision(),
		Method: input.footprint.Estimate().Method(), State: input.footprint.Estimate().State(),
		Tokens: tokenValue(input.footprint.Estimate()), HasTokens: hasTokenValue(input.footprint.Estimate()),
	}, MaxSegmentBytes)
	if err != nil {
		return Plan{}, err
	}
	inputJSON, err := codec.MarshalCanonical(struct {
		Kind           CurrentInputKind `json:"kind"`
		Text           string           `json:"text,omitempty"`
		SchemaRevision string           `json:"schema_revision"`
	}{input.currentInput.kind, input.currentInput.text, currentInputRevision}, MaxSegmentBytes)
	if err != nil {
		return Plan{}, err
	}
	profile, err := NewSegment(string(SourceProviderProfile), StabilityStable, providerProfileRevision, profileJSON)
	if err != nil {
		return Plan{}, err
	}
	toolSegment, err := NewSegment(string(SourceToolCatalog), StabilityStable, toolView.Fingerprint(), toolJSON)
	if err != nil {
		return Plan{}, err
	}
	segments := []Segment{profile, toolSegment}
	if input.projectInstructions.HasDocuments() {
		projectJSON, marshalErr := codec.MarshalCanonical(
			json.RawMessage(input.projectInstructions.CanonicalJSON()), MaxSegmentBytes,
		)
		if marshalErr != nil {
			return Plan{}, fmt.Errorf("project instruction cache segment is invalid: %w", marshalErr)
		}
		project, projectErr := NewSegment(
			string(SourceProjectInstructions), StabilityProjectStable,
			input.projectInstructions.Revision(), projectJSON,
		)
		if projectErr != nil {
			return Plan{}, projectErr
		}
		segments = append(segments, project)
	}
	history, err := NewSegment(string(SourceCommittedHistory), StabilityTurnStable, strconv.FormatUint(input.footprint.Revision(), 10), historyJSON)
	if err != nil {
		return Plan{}, err
	}
	current, err := NewSegment(string(SourceCurrentInput), StabilityVolatile, currentInputRevision, inputJSON)
	if err != nil {
		return Plan{}, err
	}
	segments = append(segments, history, current)
	return NewPlan(segments...)
}

func estimateSemanticHistory(history domain.SemanticHistoryView) domain.TokenEstimate {
	var tokens uint64
	for _, turn := range history.Turns {
		user, _ := estimate.String(turn.UserText).Tokens()
		assistant, _ := estimate.String(turn.AssistantText).Tokens()
		tokens = estimate.SaturatingAdd(tokens, user, assistant)
	}
	return mustEstimated(tokens)
}

func coverEstimates(first, second domain.TokenEstimate) (domain.TokenEstimate, error) {
	if first.Method() != second.Method() {
		return domain.TokenEstimate{}, fmt.Errorf("token estimate methods do not match")
	}
	firstTokens, firstKnown := first.Tokens()
	secondTokens, secondKnown := second.Tokens()
	if !firstKnown || !secondKnown {
		return domain.NewUnknownTokenEstimate(first.Method())
	}
	if secondTokens > firstTokens {
		firstTokens = secondTokens
	}
	return domain.NewEstimatedTokenEstimate(first.Method(), firstTokens)
}

func sumEstimates(values ...domain.TokenEstimate) domain.TokenEstimate {
	var total uint64
	for _, value := range values {
		tokens, known := value.Tokens()
		if !known {
			unknown, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
			return unknown
		}
		total = estimate.SaturatingAdd(total, tokens)
	}
	return mustEstimated(total)
}

func decideBudget(budget Budget, total domain.TokenEstimate) BudgetDecision {
	limit, enabled := budget.EffectiveInputLimit()
	if !enabled {
		return BudgetDecision{state: BudgetNotEnforced}
	}
	tokens, known := total.Tokens()
	if !known {
		return BudgetDecision{state: BudgetIndeterminate, effectiveLimit: limit}
	}
	if tokens > limit {
		return BudgetDecision{state: BudgetOverLimit, effectiveLimit: limit}
	}
	return BudgetDecision{state: BudgetWithinLimit, effectiveLimit: limit}
}

func mustEstimated(tokens uint64) domain.TokenEstimate {
	value, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, tokens)
	return value
}

func cloneSemanticHistory(history domain.SemanticHistoryView) domain.SemanticHistoryView {
	turns := make([]domain.SemanticTurn, len(history.Turns))
	copy(turns, history.Turns)
	return domain.SemanticHistoryView{
		Provider: history.Provider,
		Turns:    turns,
	}
}

func tokenValue(value domain.TokenEstimate) uint64 {
	tokens, _ := value.Tokens()
	return tokens
}

func hasTokenValue(value domain.TokenEstimate) bool {
	_, ok := value.Tokens()
	return ok
}

func knownSourceKind(kind SourceKind) bool {
	return kind == SourceProviderProfile || kind == SourceToolCatalog || kind == SourceProjectInstructions ||
		kind == SourceCommittedHistory || kind == SourceCurrentInput
}

func knownSourceLifecycle(lifecycle SourceLifecycle) bool {
	return lifecycle == SourceLifecycleReplace || lifecycle == SourceLifecycleAppend
}
