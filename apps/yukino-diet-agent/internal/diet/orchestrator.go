package diet

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// chitchatReply is the fixed guidance reply for non-diet turns; no LLM
// call is made.
const chitchatReply = "I mainly help you decide what to eat. Tell me the meal time, the taste you want, or whether you prefer something light or filling."

// Orchestrator drives one chat turn through the multi-agent pipeline:
// load session -> trace -> lock -> record message -> intent -> route ->
// recommend/clarify/plan -> persist.
type Orchestrator struct {
	sessions      *SessionService
	sessionStates *SessionStateService
	intentAgent   *IntentAgentService
	intentRevise  IntentReviseService
	slotMerge     SlotMergeService
	clarifyAgent  *ClarifyAgentService
	mealSearch    *MealSearchService
	mealRank      MealRankService
	recommend     *RecommendResponseAgentService
	mealPlan      *MealPlanService
	planResponse  *PlanResponseAgentService
	meals         *MealService
	riskGuard     RiskGuardService
	trace         *TraceService

	lockMu       sync.Mutex
	sessionLocks map[string]*sessionLock
}

// sessionLock is a reference-counted per-session mutex. The count lets the
// orchestrator drop the entry once no goroutine holds or waits for it, so
// the lock map does not grow without bound as sessions accumulate (the
// Java implementation never evicted its sessionLocks entries).
type sessionLock struct {
	mu   sync.Mutex
	refs int
}

// NewOrchestrator wires the orchestrator with all services.
func NewOrchestrator(
	sessions *SessionService,
	sessionStates *SessionStateService,
	intentAgent *IntentAgentService,
	clarifyAgent *ClarifyAgentService,
	slotMerge SlotMergeService,
	mealSearch *MealSearchService,
	mealRank MealRankService,
	recommend *RecommendResponseAgentService,
	mealPlan *MealPlanService,
	planResponse *PlanResponseAgentService,
	meals *MealService,
	riskGuard RiskGuardService,
	trace *TraceService,
) *Orchestrator {
	return &Orchestrator{
		sessions:      sessions,
		sessionStates: sessionStates,
		intentAgent:   intentAgent,
		intentRevise:  IntentReviseService{},
		slotMerge:     slotMerge,
		clarifyAgent:  clarifyAgent,
		mealSearch:    mealSearch,
		mealRank:      mealRank,
		recommend:     recommend,
		mealPlan:      mealPlan,
		planResponse:  planResponse,
		meals:         meals,
		riskGuard:     riskGuard,
		trace:         trace,
		sessionLocks:  map[string]*sessionLock{},
	}
}

// NewTraceID generates a trace_<32 hex> identifier for one turn.
func NewTraceID() string {
	return "trace_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// DietChat processes one user turn synchronously and returns the complete
// response.
func (o *Orchestrator) DietChat(ctx context.Context, userID int64, request ChatRequest) (*ChatResponse, error) {
	traceID := NewTraceID()
	if strings.TrimSpace(request.Message) == "" {
		return nil, NewError("user message is required")
	}
	// Reject unknown source modes instead of silently treating them as
	// PUBLIC; the parsed value also normalizes case and whitespace.
	sourceMode, ok := ParseSourceMode(string(request.SourceMode))
	if !ok {
		return nil, NewError("sourceMode is required, choose PERSONAL or PUBLIC")
	}
	request.SourceMode = sourceMode
	initialState, err := o.sessionStates.LoadOrCreate(request.SessionID, userID, request.SourceMode)
	if err != nil {
		return nil, err
	}
	ctx, scope := o.trace.OpenTrace(ctx, traceID, initialState.SessionID, userID)
	defer scope.Close()

	startedAt := time.Now()
	o.trace.RecordEvent(ctx, "REQUEST_RECEIVED", "HTTP", request, initialState)

	lock := o.acquireSessionLock(initialState.SessionID)
	lock.mu.Lock()
	defer o.releaseSessionLock(initialState.SessionID, lock)

	response, err := o.handleTurn(ctx, userID, request, traceID, initialState)
	if err != nil {
		o.trace.RecordError(ctx, "REQUEST_FAILED", "HTTP", request, err)
		return nil, err
	}
	o.trace.RecordEventWithLatency(ctx, "REQUEST_FINISHED", "HTTP", request, response, time.Since(startedAt).Milliseconds())
	return response, nil
}

// acquireSessionLock returns the per-session mutex that serializes
// concurrent turns of one session, registering the caller as a holder so
// the entry survives until every waiter released it.
func (o *Orchestrator) acquireSessionLock(sessionID string) *sessionLock {
	o.lockMu.Lock()
	defer o.lockMu.Unlock()
	lock, ok := o.sessionLocks[sessionID]
	if !ok {
		lock = &sessionLock{}
		o.sessionLocks[sessionID] = lock
	}
	lock.refs++
	return lock
}

// releaseSessionLock unlocks the session mutex and evicts the map entry
// once no goroutine holds or waits for it anymore.
func (o *Orchestrator) releaseSessionLock(sessionID string, lock *sessionLock) {
	lock.mu.Unlock()
	o.lockMu.Lock()
	lock.refs--
	if lock.refs <= 0 {
		delete(o.sessionLocks, sessionID)
	}
	o.lockMu.Unlock()
}

// handleTurn runs the state machine inside the session lock: record the
// user message, pre-checks, intent recognition, and routing.
func (o *Orchestrator) handleTurn(ctx context.Context, userID int64, request ChatRequest, traceID string, state SessionState) (*ChatResponse, error) {
	sessionID := state.SessionID
	sourceMode := state.SourceMode

	if err := o.sessions.AppendMessage(sessionID, "user", request.Message, nil, traceID); err != nil {
		return nil, err
	}
	o.trace.RecordEvent(ctx, "USER_MESSAGE_RECORDED", "SESSION", request.Message, map[string]any{
		"sessionId":  sessionID,
		"sourceMode": sourceMode,
	})

	if sourceMode == SourcePersonal {
		hasMeals, err := o.meals.HasPersonalMeals(userID)
		if err != nil {
			return nil, err
		}
		if !hasMeals {
			response := TextOnlyResponse("You have not added any personal meals yet. Add a few dishes you often eat first, then I can recommend from your own menu.")
			o.trace.RecordEvent(ctx, "PERSONAL_LIBRARY_EMPTY", "ROUTE", map[string]any{"userId": userID}, response)
			return o.completeTextOnly(ctx, sessionID, traceID, state, IntentMealRecommendation, response)
		}
	}

	history, err := o.sessions.RecentConversationTurns(sessionID, userID, 3)
	if err != nil {
		return nil, err
	}
	rawIntent := o.intentAgent.Recognize(ctx, sessionID, userID, request.Message, state.Slots, history)
	o.trace.RecordEvent(ctx, "INTENT_RECOGNIZED", "INTENT", request.Message, rawIntent)

	intent := o.intentRevise.Revise(state, &rawIntent, request.Message)
	o.trace.RecordEvent(ctx, "INTENT_REVISED", "INTENT", rawIntent, intent)
	o.trace.RecordEvent(ctx, "ROUTE_SELECTED", "ROUTE", intent, map[string]any{"route": intent.Intent})

	switch intent.Intent {
	case IntentMealRecommendation, IntentClarifyNeeded:
		return o.handleRecommendation(ctx, sessionID, userID, request.Message, traceID, state, intent)
	case IntentMealAdjust:
		return o.handleAdjust(ctx, sessionID, userID, request.Message, traceID, state, intent)
	case IntentMealPlan:
		return o.handlePlan(ctx, sessionID, userID, request.Message, traceID, state, intent)
	case IntentHealthRisk:
		return o.handleHealthRisk(ctx, sessionID, traceID, state)
	default:
		return o.handleChitchat(ctx, sessionID, traceID, state)
	}
}

// handleRecommendation merges slots, asks the clarify node, and either
// returns a follow-up question or enters the recommendation pipeline.
func (o *Orchestrator) handleRecommendation(ctx context.Context, sessionID string, userID int64, userInput, traceID string, state SessionState, intent IntentResult) (*ChatResponse, error) {
	mergedSlots := o.slotMerge.Merge(state.Slots, intent.Slots)
	o.trace.RecordEvent(ctx, "SLOTS_MERGED", "SLOT", map[string]any{
		"stateSlots":  state.Slots,
		"intentSlots": intent.Slots,
	}, mergedSlots)

	workingState := state.WithIntent(IntentMealRecommendation).WithSlots(mergedSlots)

	clarify := o.clarifyAgent.Decide(ctx, sessionID, userInput, mergedSlots)
	o.trace.RecordEvent(ctx, "CLARIFY_DECISION", "CLARIFY", mergedSlots, clarify)

	if clarify.Action == ClarifyAsk {
		return o.completeAsk(ctx, sessionID, traceID, workingState, clarify)
	}
	return o.completeRecommendation(ctx, sessionID, userID, userInput, traceID, workingState.WithPhase(PhaseRecommend), nil)
}

func (o *Orchestrator) completeAsk(ctx context.Context, sessionID, traceID string, workingState SessionState, clarify ClarifyResult) (*ChatResponse, error) {
	clarifyState := workingState.WithPhase(PhaseClarify)
	if err := o.sessionStates.Save(clarifyState); err != nil {
		return nil, err
	}
	clarifyIntent := IntentClarifyNeeded
	if err := o.sessions.AppendMessage(sessionID, "assistant", clarify.QuestionToAsk, &clarifyIntent, traceID); err != nil {
		return nil, err
	}
	response := ClarifyChatResponse(sessionID, traceID, clarify.QuestionToAsk, clarify.MissingSlots)
	o.trace.RecordEvent(ctx, "RESPONSE_READY", "CLARIFY", clarify, response)
	return &response, nil
}

// handleAdjust excludes previously recommended meals and reruns the
// recommendation pipeline.
func (o *Orchestrator) handleAdjust(ctx context.Context, sessionID string, userID int64, userInput, traceID string, state SessionState, intent IntentResult) (*ChatResponse, error) {
	mergedSlots := o.slotMerge.Merge(state.Slots, intent.Slots)
	excludeMealIDs := state.LastRecommendations
	if excludeMealIDs == nil {
		excludeMealIDs = []int64{}
	}
	o.trace.RecordEvent(ctx, "ADJUST_CONTEXT_RESOLVED", "ADJUST", intent, map[string]any{
		"mergedSlots":    mergedSlots,
		"excludeMealIds": excludeMealIDs,
	})
	workingState := state.WithIntent(IntentMealAdjust).WithSlots(mergedSlots).WithPhase(PhaseRecommend)
	return o.completeRecommendation(ctx, sessionID, userID, userInput, traceID, workingState, excludeMealIDs)
}

// handlePlan resolves the plan meal times and enters the plan pipeline.
func (o *Orchestrator) handlePlan(ctx context.Context, sessionID string, userID int64, userInput, traceID string, state SessionState, intent IntentResult) (*ChatResponse, error) {
	mergedSlots := o.slotMerge.Merge(state.Slots, intent.Slots)
	planMealTimes := o.mealPlan.ResolveMealTimes(mergedSlots)
	planSlots := mergedSlots.WithMealTime(planMealTimes)
	o.trace.RecordEvent(ctx, "PLAN_CONTEXT_RESOLVED", "PLAN", intent, map[string]any{
		"mergedSlots":   mergedSlots,
		"planMealTimes": planMealTimes,
		"planSlots":     planSlots,
	})
	workingState := state.WithIntent(IntentMealPlan).WithSlots(planSlots).WithPhase(PhasePlan)
	return o.completePlan(ctx, sessionID, userID, userInput, traceID, workingState, planMealTimes)
}

// completePlan searches and ranks one meal per meal time, wraps the plan
// with the plan response agent, applies the risk guard, and persists.
func (o *Orchestrator) completePlan(ctx context.Context, sessionID string, userID int64, userInput, traceID string, state SessionState, planMealTimes []string) (*ChatResponse, error) {
	plannedMeals, err := o.mealPlan.PlanMeals(state.SourceMode, userID, state.Slots, planMealTimes)
	if err != nil {
		return nil, err
	}
	planTrace := planTracePayload(plannedMeals)
	o.trace.RecordEvent(ctx, "MEAL_PLAN_SEARCHED", "PLAN", map[string]any{
		"planMealTimes": planMealTimes,
		"slots":         state.Slots,
	}, map[string]any{
		"plannedCount": len(plannedMeals),
		"plannedMeals": planTrace,
	})

	anyMatched := false
	for _, planned := range plannedMeals {
		if planned.Matched() {
			anyMatched = true
			break
		}
	}
	if !anyMatched {
		message := "The public meal library cannot form a complete multi-meal plan yet. You can add taste or cuisine preferences, or switch to personal mode and try again."
		if state.SourceMode == SourcePersonal {
			message = "Your personal meal library cannot form a multi-meal plan yet. You can add more dishes, or switch to the public meal data."
		}
		empty := TextOnlyResponse(message)
		o.trace.RecordEvent(ctx, "NO_MEAL_PLAN_MATCHED", "PLAN", state, empty)
		return o.completeTextOnly(ctx, sessionID, traceID, state, IntentMealPlan, empty)
	}

	recommend, response := o.planResponse.PlanAndRespond(ctx, sessionID, userInput, state.SourceMode, state.Slots, plannedMeals)
	o.trace.RecordEvent(ctx, "PLAN_RESULT_BUILT", "PLAN", map[string]any{
		"strategy":     string(IntentMealPlan),
		"plannedMeals": planTrace,
	}, recommend)
	o.trace.RecordEvent(ctx, "PLAN_RESPONSE_AGENT_RESULT", "RESPONSE", recommend, response)

	guard := o.riskGuard.Check(userInput, IntentMealPlan, response)
	o.trace.RecordEvent(ctx, "NUTRITION_GUARD_CHECKED", "GUARD", map[string]any{
		"intent":   IntentMealPlan,
		"response": response,
	}, guard)
	if !guard.Passed {
		response = TextOnlyResponse(guard.RewriteSuggestion)
		o.trace.RecordEvent(ctx, "NUTRITION_GUARD_REWRITTEN", "GUARD", guard, response)
	} else {
		o.trace.RecordEvent(ctx, "COMPLIANCE_GUARD_REWRITTEN", "GUARD", nil, response)
	}

	lastIDs := recommendationIDs(recommend)
	savedState := state.AppendLastRecommendations(lastIDs)
	if err := o.sessionStates.Save(savedState); err != nil {
		return nil, err
	}
	planIntent := IntentMealPlan
	if err := o.sessions.AppendMessage(sessionID, "assistant", response.SpeechText, &planIntent, traceID); err != nil {
		return nil, err
	}
	chatResponse := AnswerChatResponse(sessionID, traceID, response.SpeechText, response.DisplayBlocks, response.NextAction)
	o.trace.RecordEvent(ctx, "RESPONSE_READY", "RESPONSE", savedState, chatResponse)
	return &chatResponse, nil
}

// handleHealthRisk returns the conservative guard message without any
// recommendation.
func (o *Orchestrator) handleHealthRisk(ctx context.Context, sessionID, traceID string, state SessionState) (*ChatResponse, error) {
	response := TextOnlyResponse(ConservativeMessage)
	return o.completeTextOnly(ctx, sessionID, traceID, state, IntentHealthRisk, response)
}

// handleChitchat returns the fixed guidance reply without an LLM call.
func (o *Orchestrator) handleChitchat(ctx context.Context, sessionID, traceID string, state SessionState) (*ChatResponse, error) {
	response := TextOnlyResponse(chitchatReply)
	return o.completeTextOnly(ctx, sessionID, traceID, state, IntentOther, response)
}

// completeRecommendation is the full pipeline: search -> rank -> LLM
// reasons and speech -> guard -> persist.
func (o *Orchestrator) completeRecommendation(ctx context.Context, sessionID string, userID int64, userInput, traceID string, state SessionState, excludeMealIDs []int64) (*ChatResponse, error) {
	candidates, err := o.mealSearch.Search(state.SourceMode, userID, state.Slots)
	if err != nil {
		return nil, err
	}
	o.trace.RecordEvent(ctx, "MEAL_SEARCHED", "SEARCH", state.Slots, map[string]any{
		"candidateCount": len(candidates),
		"candidates":     candidates,
	})

	ranked := o.mealRank.Rank(candidates, state.Slots, excludeMealIDs)
	o.trace.RecordEvent(ctx, "MEAL_RANKED", "RANK", map[string]any{
		"excludeMealIds": excludeMealIDs,
	}, map[string]any{
		"rankedCount": len(ranked),
		"ranked":      ranked,
	})

	if len(ranked) == 0 {
		message := "The public meal library has no close matches right now. You can switch to personal mode or add a meal time, taste, or cuisine."
		if state.SourceMode == SourcePersonal {
			message = "Your personal meal library has no matches right now. You can add more dishes, or switch to the public meal data."
		}
		empty := TextOnlyResponse(message)
		o.trace.RecordEvent(ctx, "NO_MEAL_MATCHED", "RECOMMEND", state, empty)
		return o.completeTextOnly(ctx, sessionID, traceID, state, state.CurrentIntent, empty)
	}

	recommend, response := o.recommend.RecommendAndRespond(ctx, sessionID, userInput, state.SourceMode, state.Slots, ranked)
	o.trace.RecordEvent(ctx, "RECOMMEND_RESULT_BUILT", "RECOMMEND", map[string]any{
		"strategy": strategyName(state.CurrentIntent),
		"ranked":   ranked,
	}, recommend)
	o.trace.RecordEvent(ctx, "RESPONSE_AGENT_RESULT", "RESPONSE", recommend, response)

	guard := o.riskGuard.Check(userInput, state.CurrentIntent, response)
	o.trace.RecordEvent(ctx, "NUTRITION_GUARD_CHECKED", "GUARD", map[string]any{
		"intent":   state.CurrentIntent,
		"response": response,
	}, guard)
	if !guard.Passed {
		response = TextOnlyResponse(guard.RewriteSuggestion)
		o.trace.RecordEvent(ctx, "NUTRITION_GUARD_REWRITTEN", "GUARD", guard, response)
	} else {
		o.trace.RecordEvent(ctx, "COMPLIANCE_GUARD_REWRITTEN", "GUARD", nil, response)
	}

	lastIDs := recommendationIDs(recommend)
	savedState := state.AppendLastRecommendations(lastIDs)
	if err := o.sessionStates.Save(savedState); err != nil {
		return nil, err
	}
	var currentIntent *Intent
	if state.CurrentIntent != "" {
		intent := state.CurrentIntent
		currentIntent = &intent
	}
	if err := o.sessions.AppendMessage(sessionID, "assistant", response.SpeechText, currentIntent, traceID); err != nil {
		return nil, err
	}
	chatResponse := AnswerChatResponse(sessionID, traceID, response.SpeechText, response.DisplayBlocks, response.NextAction)
	o.trace.RecordEvent(ctx, "RESPONSE_READY", "RESPONSE", savedState, chatResponse)
	return &chatResponse, nil
}

// completeTextOnly finishes a plain-text branch: update intent, save
// state, persist the assistant message, and return the response.
func (o *Orchestrator) completeTextOnly(ctx context.Context, sessionID, traceID string, state SessionState, intent Intent, response ResponseResult) (*ChatResponse, error) {
	savedState := state.WithIntent(intent)
	if err := o.sessionStates.Save(savedState); err != nil {
		return nil, err
	}
	if err := o.sessions.AppendMessage(sessionID, "assistant", response.SpeechText, &intent, traceID); err != nil {
		return nil, err
	}
	chatResponse := AnswerChatResponse(sessionID, traceID, response.SpeechText, response.DisplayBlocks, response.NextAction)
	o.trace.RecordEvent(ctx, "RESPONSE_READY", "RESPONSE", map[string]any{
		"intent": intent,
		"state":  savedState,
	}, chatResponse)
	return &chatResponse, nil
}

func strategyName(intent Intent) string {
	if intent == "" {
		return string(IntentMealRecommendation)
	}
	return string(intent)
}

func recommendationIDs(recommend RecommendResult) []int64 {
	ids := make([]int64, 0, len(recommend.Recommendations))
	for _, option := range recommend.Recommendations {
		ids = append(ids, option.ItemID)
	}
	return ids
}

func planTracePayload(plannedMeals []PlannedMeal) []map[string]any {
	payload := make([]map[string]any, 0, len(plannedMeals))
	for _, planned := range plannedMeals {
		entry := map[string]any{
			"mealTime": planned.MealTime,
			"matched":  planned.Matched(),
		}
		if planned.Matched() {
			entry["mealId"] = planned.Meal.ID
			entry["mealName"] = planned.Meal.Name
		}
		payload = append(payload, entry)
	}
	return payload
}
