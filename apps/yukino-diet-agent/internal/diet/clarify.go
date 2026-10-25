package diet

import (
	"context"
	"fmt"
	"strings"
)

// ClarifyRuleService decides whether to ask the user for more details.
// The decision is rule-based so LLM randomness cannot drive the state
// machine; the clarify agent only phrases the question.
type ClarifyRuleService struct{}

// HasEnoughSlots reports whether the slots are sufficient to recommend.
func (r ClarifyRuleService) HasEnoughSlots(slots SlotBundle) bool {
	return len(r.MissingSlots(slots)) == 0
}

// MissingSlots computes the missing required slots: mealTime is always
// required; healthGoal is required unless a strong food preference
// (cuisine/taste/scene/convenience) is present.
func (r ClarifyRuleService) MissingSlots(slots SlotBundle) []string {
	missing := []string{}
	if len(slots.MealTime) == 0 {
		missing = append(missing, "mealTime")
	}
	if len(slots.HealthGoal) == 0 && !hasStrongFoodPreference(slots) {
		missing = append(missing, "healthGoal")
	}
	return missing
}

func hasStrongFoodPreference(slots SlotBundle) bool {
	return len(slots.Cuisine) > 0 || len(slots.Taste) > 0 ||
		len(slots.Scene) > 0 || len(slots.Convenience) > 0
}

// FallbackQuestion renders the template question used when the LLM is
// unavailable or returns an empty question.
func (r ClarifyRuleService) FallbackQuestion(missingSlots []string) string {
	if len(missingSlots) == 0 {
		return "Would you rather pick by taste, or by a goal like something light or filling?"
	}
	if containsString(missingSlots, "mealTime") {
		return "Is this meal mainly breakfast, lunch, or dinner?"
	}
	if containsString(missingSlots, "healthGoal") {
		return "Would you like something light, something filling, or just pick by taste?"
	}
	return "Just to confirm, what matters most for this meal: taste, health goals, or convenience?"
}

// ClarifyAgentService combines the clarify rule with the clarify agent:
// rules decide ASK/READY, and only ASK triggers an LLM call to phrase the
// follow-up question naturally.
type ClarifyAgentService struct {
	agents *AgentSet
	rule   ClarifyRuleService
	trace  *TraceService
}

// NewClarifyAgentService builds the clarify agent service.
func NewClarifyAgentService(agents *AgentSet, trace *TraceService) *ClarifyAgentService {
	return &ClarifyAgentService{agents: agents, trace: trace}
}

// Decide returns READY when no required slot is missing, otherwise ASK
// with an LLM-phrased (or template) question.
func (s *ClarifyAgentService) Decide(ctx context.Context, sessionID, userInput string, slots SlotBundle) ClarifyResult {
	missingSlots := s.rule.MissingSlots(slots)
	if len(missingSlots) == 0 {
		return ReadyClarifyResult()
	}
	prompt := fmt.Sprintf(`User's words: %s
Known info: %s
Missing fields: %s
`, userInput, jsonString(slots), strings.Join(missingSlots, ", "))
	reply, err := s.trace.CallAgent(ctx, sessionID, "ClarifyAgent", s.agents.Clarify, prompt)
	if err != nil {
		return AskClarifyResult(s.rule.FallbackQuestion(missingSlots), missingSlots)
	}
	question := strings.TrimSpace(reply.Text)
	if question == "" {
		question = s.rule.FallbackQuestion(missingSlots)
	}
	return AskClarifyResult(question, missingSlots)
}
