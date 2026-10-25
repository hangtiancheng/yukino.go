package diet

import (
	"context"
	"fmt"
	"strings"
)

// disclaimerKeywords are health goals that require the health disclaimer
// suffix on the final reply.
var disclaimerKeywords = []string{"fat loss", "low sugar", "low carb", "stomach-friendly"}

// RecommendResponseAgentService generates recommendation reasons and the
// spoken reply in a single LLM call, with template fallbacks.
type RecommendResponseAgentService struct {
	agents *AgentSet
	trace  *TraceService
}

// NewRecommendResponseAgentService builds the recommend response service.
func NewRecommendResponseAgentService(agents *AgentSet, trace *TraceService) *RecommendResponseAgentService {
	return &RecommendResponseAgentService{agents: agents, trace: trace}
}

// RecommendAndRespond turns the ranked candidates into a RecommendResult
// plus the user-facing ResponseResult. LLM failures degrade to template
// reasons and template speech instead of failing the turn.
func (s *RecommendResponseAgentService) RecommendAndRespond(
	ctx context.Context,
	sessionID string,
	userInput string,
	sourceMode SourceMode,
	slots SlotBundle,
	rankedMeals []MealItem,
) (RecommendResult, ResponseResult) {
	topMeals := rankedMeals
	if len(topMeals) > 3 {
		topMeals = topMeals[:3]
	}
	if len(topMeals) == 0 {
		return EmptyRecommendResult(), TextOnlyResponse(
			"No closely matching meals were found yet. You can add meals with the matching features first.")
	}
	needDisclaimer := NeedsDisclaimer(slots)
	prompt := fmt.Sprintf(`User's words: %s
Data source mode: %s
Slots of this turn: %s
Candidate meals: %s
Output JSON with a recommendations array (each item has mealId and reason) and speechText; never invent meals outside the candidates.
`, userInput, sourceMode, jsonString(slots), jsonString(topMeals))

	reply, err := s.trace.CallAgent(ctx, sessionID, "RecommendResponseAgent", s.agents.RecommendResponse, prompt)
	if err != nil {
		recommend := RecommendResult{Recommendations: templateOptions(topMeals, slots), NeedDisclaimer: needDisclaimer}
		return recommend, ResponseResult{
			SpeechText:    TemplateRecommendSpeech(recommend),
			DisplayBlocks: ToDisplayBlocks(recommend),
			NextAction:    "WAIT_USER",
		}
	}
	options, speechText := parseRecommendOutput(reply.Text, topMeals, slots)
	recommend := RecommendResult{Recommendations: options, NeedDisclaimer: needDisclaimer}
	if strings.TrimSpace(speechText) == "" {
		speechText = TemplateRecommendSpeech(recommend)
	}
	return recommend, ResponseResult{
		SpeechText:    speechText,
		DisplayBlocks: ToDisplayBlocks(recommend),
		NextAction:    "WAIT_USER",
	}
}

// parseRecommendOutput extracts the recommendation reasons and speech
// text from the LLM JSON output, keeping only candidate meal IDs.
func parseRecommendOutput(content string, topMeals []MealItem, slots SlotBundle) ([]RecommendedMealOption, string) {
	byID := make(map[int64]MealItem, len(topMeals))
	for _, meal := range topMeals {
		byID[meal.ID] = meal
	}
	reasons := map[int64]string{}
	speechText := ""
	root, err := ExtractJSONObject(content)
	if err == nil {
		if recommendations, ok := root["recommendations"].([]any); ok {
			for _, entry := range recommendations {
				item, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				mealID := firstInt64(item, "mealId", "itemId")
				// reason may arrive as a non-string scalar; coerce it the way
				// the Java implementation's asText did instead of dropping it.
				reason := jsonText(item["reason"])
				if _, known := byID[mealID]; known && strings.TrimSpace(reason) != "" {
					reasons[mealID] = strings.TrimSpace(reason)
				}
			}
		}
		speechText = strings.TrimSpace(jsonText(root["speechText"]))
	}
	options := make([]RecommendedMealOption, 0, len(topMeals))
	for _, meal := range topMeals {
		reason, ok := reasons[meal.ID]
		if !ok {
			reason = TemplateMealReason(meal, slots)
		}
		options = append(options, RecommendedMealOption{
			ItemID:       meal.ID,
			SourceType:   meal.SourceType,
			Name:         meal.Name,
			Reason:       reason,
			MatchScore:   meal.MatchScore,
			MatchedSlots: meal.Slots,
		})
	}
	return options, speechText
}

func firstInt64(item map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := jsonInt64(item[key]); ok {
			return value
		}
	}
	return 0
}

func templateOptions(topMeals []MealItem, slots SlotBundle) []RecommendedMealOption {
	options := make([]RecommendedMealOption, 0, len(topMeals))
	for _, meal := range topMeals {
		options = append(options, RecommendedMealOption{
			ItemID:       meal.ID,
			SourceType:   meal.SourceType,
			Name:         meal.Name,
			Reason:       TemplateMealReason(meal, slots),
			MatchScore:   meal.MatchScore,
			MatchedSlots: meal.Slots,
		})
	}
	return options
}

// TemplateMealReason renders the fallback reason for one meal.
func TemplateMealReason(meal MealItem, slots SlotBundle) string {
	if len(slots.HealthGoal) > 0 {
		return fmt.Sprintf("%s fits your %s goals fairly well.", meal.Name, strings.Join(slots.HealthGoal, ", "))
	}
	if len(slots.Taste) > 0 {
		return fmt.Sprintf("%s is close to the %s taste you asked for.", meal.Name, strings.Join(slots.Taste, ", "))
	}
	return fmt.Sprintf("%s matches the dining preferences you expressed this round.", meal.Name)
}

// TemplateRecommendSpeech renders the fallback spoken reply, appending the
// health disclaimer when required.
func TemplateRecommendSpeech(recommend RecommendResult) string {
	if len(recommend.Recommendations) == 0 {
		return "No closely matching meals were found yet. You can add a meal time, taste, or a goal like light or filling."
	}
	var builder strings.Builder
	builder.WriteString("I'd recommend these first:")
	for _, option := range recommend.Recommendations {
		builder.WriteString("\n- ")
		builder.WriteString(option.Name)
		builder.WriteString(": ")
		builder.WriteString(option.Reason)
	}
	builder.WriteString("\nWant something lighter, or shall we go with these?")
	if recommend.NeedDisclaimer {
		builder.WriteString(DisclaimerSuffix)
	}
	return builder.String()
}

// DisclaimerSuffix is the health boundary note appended when the slots
// touch weight loss or similar health goals.
const DisclaimerSuffix = "\nThese suggestions are for everyday diet reference only. If you have a diagnosed condition or a special physical situation, please consult a doctor or a dietitian."

// ToDisplayBlocks flattens recommendations into frontend meal cards.
func ToDisplayBlocks(recommend RecommendResult) []MealResponse {
	blocks := make([]MealResponse, 0, len(recommend.Recommendations))
	for _, option := range recommend.Recommendations {
		blocks = append(blocks, MealResponseFromOption(option))
	}
	return blocks
}

// NeedsDisclaimer reports whether the slots require the health disclaimer.
func NeedsDisclaimer(slots SlotBundle) bool {
	for _, goal := range slots.HealthGoal {
		lower := strings.ToLower(goal)
		for _, keyword := range disclaimerKeywords {
			if strings.Contains(lower, keyword) {
				return true
			}
		}
	}
	return false
}
