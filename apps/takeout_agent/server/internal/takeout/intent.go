package takeout

import (
	"context"
	"fmt"
	"strings"
)

// Keyword sets for the rule-based intent fallbacks. Matching is
// case-insensitive on word boundaries.
var (
	healthRiskKeywords = newKeywordMatcher(
		"stomachache", "stomach ache", "stomach pain", "stomach hurts",
		"diabetes", "diabetic",
		"pregnant", "pregnancy", "minor", "underage", "child", "children",
		"hypertension", "high blood pressure", "cure", "treatment", "therapy",
		"diagnose", "diagnosis", "prescription", "medication",
		"extreme dieting", "starve", "starvation", "fasting",
		"eat nothing", "only drink water", "only water",
	)
	mealAdjustKeywords = newKeywordMatcher(
		"something else", "anything else", "other options", "another batch",
		"swap", "replace", "change it", "switch", "not so oily", "too oily",
		"less oily", "lighter", "cheaper", "faster", "more convenient",
		"different ones", "don't feel like", "do not feel like",
	)
	mealPlanKeywords = newKeywordMatcher(
		"three meals", "breakfast, lunch", "breakfast lunch", "meal plan",
		"plan my meals", "plan my day", "plan today", "plan a day",
		"whole day", "full day", "a week", "weekly takeout", "daily takeout",
		"takeout plan",
	)
	otherKeywords = newKeywordMatcher(
		"who are you", "are you ai", "are you a bot", "are you human",
		"hello", "hi", "hey", "weather", "movie", "package",
	)
	recommendKeywords = newKeywordMatcher(
		"what to eat", "what should i eat", "recommend", "suggestion",
		"suggest", "dinner", "lunch", "breakfast", "supper", "snack",
		"hungry", "craving", "want to eat", "something to eat", "eat",
	)
)

// IntentAgentService calls the intent agent, parses its JSON output, and
// falls back to keyword rules when the LLM fails.
type IntentAgentService struct {
	agents      *AgentSet
	trace       *TraceService
	slotOptions *SlotOptionService
}

// NewIntentAgentService builds the intent agent service.
func NewIntentAgentService(agents *AgentSet, trace *TraceService, slotOptions *SlotOptionService) *IntentAgentService {
	return &IntentAgentService{agents: agents, trace: trace, slotOptions: slotOptions}
}

// Recognize classifies one user turn into an IntentResult. LLM or parsing
// failures degrade to the keyword fallback instead of failing the turn.
func (s *IntentAgentService) Recognize(
	ctx context.Context,
	sessionID string,
	userID int64,
	userInput string,
	knownSlots SlotBundle,
	recentHistory []ConversationTurn,
) IntentResult {
	slotOptions, err := s.slotOptions.FindAllOptions()
	if err != nil {
		return s.fallback(userInput)
	}
	prompt := buildIntentPrompt(userID, sessionID, userInput, knownSlots, recentHistory, slotOptions)
	reply, err := s.trace.CallAgent(ctx, sessionID, "IntentAgent", s.agents.Intent, prompt)
	if err != nil {
		return s.fallback(userInput)
	}
	result, err := parseIntentResult(reply.Text, userInput, slotOptions)
	if err != nil {
		return s.fallback(userInput)
	}
	return result
}

// buildIntentPrompt renders the user prompt with context and output
// constraints.
func buildIntentPrompt(
	userID int64,
	sessionID string,
	userInput string,
	knownSlots SlotBundle,
	recentHistory []ConversationTurn,
	slotOptions map[string][]string,
) string {
	return fmt.Sprintf(`userId: %d
sessionId: %s
recentHistory: %s
knownSlots: %s
slotOptions: %s
Current sentence: %s
Output JSON with the fields intent, slots, and confidence.
Every slot value must be chosen from the candidates of the matching slotOptions field; output null or an empty array when nothing maps, and never invent labels.
`,
		userID, sessionID, jsonString(recentHistory), jsonString(knownSlots), jsonString(slotOptions), userInput)
}

// parseIntentResult converts the LLM JSON output into an IntentResult,
// filtering slot labels through the dictionary.
func parseIntentResult(content, userInput string, slotOptions map[string][]string) (IntentResult, error) {
	root, err := ExtractJSONObject(content)
	if err != nil {
		return IntentResult{}, err
	}
	intent := fallbackIntent(userInput)
	if rawIntent, ok := root["intent"].(string); ok && strings.TrimSpace(rawIntent) != "" {
		if parsed, ok := ParseIntent(strings.TrimSpace(rawIntent)); ok {
			intent = parsed
		}
	}
	// Accept both a nested "slots" object and a flat layout.
	slotsNode := root
	if nested, ok := root["slots"].(map[string]any); ok {
		slotsNode = nested
	}
	slots := NewSlotBundle(
		PickSlotLabels(slotsNode, "mealTime", slotOptions["mealTime"]),
		PickSlotLabels(slotsNode, "mood", slotOptions["mood"]),
		PickSlotLabels(slotsNode, "scene", slotOptions["scene"]),
		PickSlotLabels(slotsNode, "healthGoal", slotOptions["healthGoal"]),
		PickSlotLabels(slotsNode, "cuisine", slotOptions["cuisine"]),
		PickSlotLabels(slotsNode, "taste", slotOptions["taste"]),
		PickSlotLabels(slotsNode, "convenience", slotOptions["convenience"]),
	)
	confidence := 0.5
	// Accept numeric strings as well ("0.9"); the Java implementation got
	// this tolerance from Jackson's asDouble coercion.
	if parsed, ok := jsonFloat(root["confidence"]); ok {
		confidence = parsed
	}
	return IntentResult{Intent: intent, Slots: slots, Confidence: confidence}, nil
}

// fallback builds the conservative result used when the LLM is unavailable.
func (s *IntentAgentService) fallback(userInput string) IntentResult {
	return IntentResult{Intent: fallbackIntent(userInput), Slots: EmptySlotBundle(), Confidence: 0.2}
}

// fallbackIntent infers the intent from keyword rules, in priority order.
func fallbackIntent(userInput string) Intent {
	if strings.TrimSpace(userInput) == "" {
		return IntentClarifyNeeded
	}
	if healthRiskKeywords.matchesAny(userInput) {
		return IntentHealthRisk
	}
	if mealAdjustKeywords.matchesAny(userInput) {
		return IntentMealAdjust
	}
	if mealPlanKeywords.matchesAny(userInput) {
		return IntentMealPlan
	}
	if otherKeywords.matchesAny(userInput) {
		return IntentOther
	}
	if recommendKeywords.matchesAny(userInput) {
		return IntentMealRecommendation
	}
	return IntentClarifyNeeded
}

// IntentReviseService applies rule-based corrections to the LLM intent
// using the persisted session state before routing.
type IntentReviseService struct{}

// lowConfidenceThreshold downgrades weak recommendation intents to clarify.
const lowConfidenceThreshold = 0.4

// extraMealPlanKeywords are plan phrases only used by the revise rules.
var extraMealPlanKeywords = newKeywordMatcher(
	"three meals a day", "breakfast lunch and dinner", "breakfast, lunch and dinner",
	"plan my three meals", "plan three meals", "today's meals", "meals for today",
)

// Revise corrects one IntentResult against the session state:
//  1. health-risk keywords always win;
//  2. MEAL_ADJUST without previous recommendations becomes a recommendation;
//  3. meal-plan keywords force MEAL_PLAN;
//  4. low-confidence recommendations degrade to CLARIFY_NEEDED.
func (IntentReviseService) Revise(state SessionState, result *IntentResult, userInput string) IntentResult {
	safe := result
	if safe == nil {
		clarify := ClarifyIntentResult(EmptySlotBundle())
		safe = &clarify
	}
	slots := safe.Slots

	if safe.Intent == IntentHealthRisk || healthRiskKeywords.matchesAny(userInput) {
		return IntentResult{Intent: IntentHealthRisk, Slots: slots, Confidence: safe.Confidence}
	}
	if safe.Intent == IntentMealAdjust && len(state.LastRecommendations) == 0 {
		return IntentResult{Intent: IntentMealRecommendation, Slots: slots, Confidence: safe.Confidence}
	}
	if (mealPlanKeywords.matchesAny(userInput) || extraMealPlanKeywords.matchesAny(userInput)) &&
		safe.Intent != IntentMealAdjust && safe.Intent != IntentMealPlan {
		return IntentResult{Intent: IntentMealPlan, Slots: slots, Confidence: safe.Confidence}
	}
	if safe.Intent == IntentMealRecommendation && safe.Confidence < lowConfidenceThreshold {
		return IntentResult{Intent: IntentClarifyNeeded, Slots: slots, Confidence: safe.Confidence}
	}
	return *safe
}
