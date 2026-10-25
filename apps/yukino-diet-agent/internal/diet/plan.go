package diet

import (
	"context"
	"fmt"
	"strings"
)

// DefaultPlanMealTimes is the default three-meal plan target.
var DefaultPlanMealTimes = []string{"Breakfast", "Lunch", "Dinner"}

// AggregateThreeMeals is the aggregate label expanded into concrete meal
// times.
const AggregateThreeMeals = "Three Meals"

// PlannedMeal is the per-meal-time planning result; Meal is nil when no
// candidate matched.
type PlannedMeal struct {
	MealTime   string     `json:"mealTime"`
	Meal       *MealItem  `json:"meal"`
	QuerySlots SlotBundle `json:"querySlots"`
}

// Matched reports whether a meal was picked for this meal time.
func (p PlannedMeal) Matched() bool {
	return p.Meal != nil && p.Meal.ID != 0
}

// MealPlanService resolves the target meal times and runs one
// search+rank pass per meal time, picking the top candidate each time.
type MealPlanService struct {
	search *MealSearchService
	rank   MealRankService
}

// NewMealPlanService builds the plan service.
func NewMealPlanService(search *MealSearchService) *MealPlanService {
	return &MealPlanService{search: search}
}

// ResolveMealTimes derives the plan targets from the merged slots:
// empty or containing the aggregate label falls back to the default
// breakfast/lunch/dinner; two or more concrete meal times keep the user's
// order; a single concrete meal time still plans the default three meals.
func (MealPlanService) ResolveMealTimes(slots SlotBundle) []string {
	raw := slots.MealTime
	if len(raw) == 0 {
		return DefaultPlanMealTimes
	}
	for _, value := range raw {
		if value == AggregateThreeMeals {
			return DefaultPlanMealTimes
		}
	}
	specific := []string{}
	for _, value := range raw {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || trimmed == AggregateThreeMeals {
			continue
		}
		if !containsString(specific, trimmed) {
			specific = append(specific, trimmed)
		}
	}
	if len(specific) >= 2 {
		return specific
	}
	return DefaultPlanMealTimes
}

// SlotsForMealTime copies the shared slots and pins mealTime to one value.
func (MealPlanService) SlotsForMealTime(base SlotBundle, mealTime string) SlotBundle {
	return base.WithMealTime([]string{mealTime})
}

// PlanMeals searches and ranks per meal time, taking the top candidate of
// each while excluding meals already chosen for earlier meal times.
func (s *MealPlanService) PlanMeals(sourceMode SourceMode, userID int64, baseSlots SlotBundle, mealTimes []string) ([]PlannedMeal, error) {
	targets := mealTimes
	if len(targets) == 0 {
		targets = DefaultPlanMealTimes
	}
	planned := make([]PlannedMeal, 0, len(targets))
	usedIDs := []int64{}
	for _, mealTime := range targets {
		querySlots := s.SlotsForMealTime(baseSlots, mealTime)
		candidates, err := s.search.Search(sourceMode, userID, querySlots)
		if err != nil {
			return nil, err
		}
		ranked := s.rank.Rank(candidates, querySlots, usedIDs)
		var picked *MealItem
		for i := range ranked {
			if ranked[i].ID != 0 && !containsInt64(usedIDs, ranked[i].ID) {
				meal := ranked[i]
				picked = &meal
				break
			}
		}
		if picked != nil {
			usedIDs = append(usedIDs, picked.ID)
			planned = append(planned, PlannedMeal{MealTime: mealTime, Meal: picked, QuerySlots: querySlots})
		} else {
			planned = append(planned, PlannedMeal{MealTime: mealTime, Meal: nil, QuerySlots: querySlots})
		}
	}
	return planned, nil
}

// PlanResponseAgentService wraps the per-meal-time plan into reasons and a
// spoken reply with one LLM call, with template fallbacks.
type PlanResponseAgentService struct {
	agents *AgentSet
	trace  *TraceService
}

// NewPlanResponseAgentService builds the plan response service.
func NewPlanResponseAgentService(agents *AgentSet, trace *TraceService) *PlanResponseAgentService {
	return &PlanResponseAgentService{agents: agents, trace: trace}
}

// PlanAndRespond packages the planned meals into a RecommendResult plus the
// user-facing ResponseResult.
func (s *PlanResponseAgentService) PlanAndRespond(
	ctx context.Context,
	sessionID string,
	userInput string,
	sourceMode SourceMode,
	sharedSlots SlotBundle,
	plannedMeals []PlannedMeal,
) (RecommendResult, ResponseResult) {
	matched := make([]PlannedMeal, 0, len(plannedMeals))
	for _, planned := range plannedMeals {
		if planned.Matched() {
			matched = append(matched, planned)
		}
	}
	needDisclaimer := NeedsDisclaimer(sharedSlots)
	if len(matched) == 0 {
		return EmptyRecommendResult(), TextOnlyResponse(
			"No complete multi-meal plan could be formed yet. You can add taste or cuisine preferences, or switch the data source and try again.")
	}
	prompt := buildPlanPrompt(userInput, sourceMode, sharedSlots, plannedMeals)
	reply, err := s.trace.CallAgent(ctx, sessionID, "PlanResponseAgent", s.agents.PlanResponse, prompt)
	if err != nil {
		recommend := RecommendResult{Recommendations: templatePlanOptions(plannedMeals, sharedSlots), NeedDisclaimer: needDisclaimer}
		return recommend, ResponseResult{
			SpeechText:    TemplatePlanSpeech(plannedMeals, recommend),
			DisplayBlocks: ToDisplayBlocks(recommend),
			NextAction:    "WAIT_USER",
		}
	}
	options, speechText := parsePlanOutput(reply.Text, plannedMeals, sharedSlots)
	recommend := RecommendResult{Recommendations: options, NeedDisclaimer: needDisclaimer}
	if strings.TrimSpace(speechText) == "" {
		speechText = TemplatePlanSpeech(plannedMeals, recommend)
	}
	return recommend, ResponseResult{
		SpeechText:    speechText,
		DisplayBlocks: ToDisplayBlocks(recommend),
		NextAction:    "WAIT_USER",
	}
}

func buildPlanPrompt(userInput string, sourceMode SourceMode, sharedSlots SlotBundle, plannedMeals []PlannedMeal) string {
	var mealSection strings.Builder
	for _, planned := range plannedMeals {
		mealSection.WriteString("\n- mealTime=")
		mealSection.WriteString(planned.MealTime)
		if planned.Matched() {
			meal := *planned.Meal
			fmt.Fprintf(&mealSection, ", candidate=[mealId=%d, name=%s, score=%.4f]", meal.ID, meal.Name, meal.MatchScore)
		} else {
			mealSection.WriteString(", candidate=[] (no match yet)")
		}
	}
	return fmt.Sprintf(`User's words: %s
Data source mode: %s
Shared slots: %s
Candidates per meal time: %s
Output JSON with a mealPlans array (each item has mealTime, mealId, and reason) and speechText; mealId must come from the candidates of its own meal time.
`, userInput, sourceMode, jsonString(sharedSlots), mealSection.String())
}

// parsePlanOutput extracts per-meal-time reasons and the speech text. The
// meal selection always comes from the Go planner, never from the LLM, so
// meal IDs cannot leak across meal times.
func parsePlanOutput(content string, plannedMeals []PlannedMeal, sharedSlots SlotBundle) ([]RecommendedMealOption, string) {
	reasonsByMealTime := map[string]string{}
	speechText := ""
	root, err := ExtractJSONObject(content)
	if err == nil {
		if plans, ok := root["mealPlans"].([]any); ok {
			for _, entry := range plans {
				item, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				mealTime := strings.TrimSpace(jsonText(item["mealTime"]))
				reason := strings.TrimSpace(jsonText(item["reason"]))
				if mealTime != "" && reason != "" {
					reasonsByMealTime[mealTime] = reason
				}
			}
		}
		speechText = strings.TrimSpace(jsonText(root["speechText"]))
	}
	options := make([]RecommendedMealOption, 0, len(plannedMeals))
	for _, planned := range plannedMeals {
		if !planned.Matched() {
			continue
		}
		meal := *planned.Meal
		reason, ok := reasonsByMealTime[planned.MealTime]
		if !ok {
			reason = templatePlanReason(planned, sharedSlots)
		}
		options = append(options, RecommendedMealOption{
			ItemID:       meal.ID,
			SourceType:   meal.SourceType,
			Name:         meal.Name,
			Reason:       reason,
			MatchScore:   meal.MatchScore,
			MatchedSlots: planned.QuerySlots,
		})
	}
	return options, speechText
}

func templatePlanOptions(plannedMeals []PlannedMeal, sharedSlots SlotBundle) []RecommendedMealOption {
	options := make([]RecommendedMealOption, 0, len(plannedMeals))
	for _, planned := range plannedMeals {
		if !planned.Matched() {
			continue
		}
		meal := *planned.Meal
		options = append(options, RecommendedMealOption{
			ItemID:       meal.ID,
			SourceType:   meal.SourceType,
			Name:         meal.Name,
			Reason:       templatePlanReason(planned, sharedSlots),
			MatchScore:   meal.MatchScore,
			MatchedSlots: planned.QuerySlots,
		})
	}
	return options
}

func templatePlanReason(planned PlannedMeal, sharedSlots SlotBundle) string {
	meal := *planned.Meal
	if len(sharedSlots.HealthGoal) > 0 {
		return fmt.Sprintf("%s fits your %s goals fairly well and works for %s.", meal.Name, strings.Join(sharedSlots.HealthGoal, ", "), planned.MealTime)
	}
	if len(sharedSlots.Taste) > 0 {
		return fmt.Sprintf("%s is close to the %s taste you asked for and suits %s.", meal.Name, strings.Join(sharedSlots.Taste, ", "), planned.MealTime)
	}
	return fmt.Sprintf("%s matches your preferences well and is a good fit for %s.", meal.Name, planned.MealTime)
}

// TemplatePlanSpeech renders the fallback spoken reply for a multi-meal
// plan.
func TemplatePlanSpeech(plannedMeals []PlannedMeal, recommend RecommendResult) string {
	var builder strings.Builder
	builder.WriteString("Here is a plan arranged by meal time:")
	for _, planned := range plannedMeals {
		builder.WriteString("\n- ")
		builder.WriteString(planned.MealTime)
		builder.WriteString(": ")
		if planned.Matched() {
			meal := *planned.Meal
			reason := meal.Name
			for _, option := range recommend.Recommendations {
				if option.ItemID == meal.ID {
					reason = option.Reason
					break
				}
			}
			builder.WriteString(meal.Name)
			builder.WriteString(" (")
			builder.WriteString(reason)
			builder.WriteString(")")
		} else {
			builder.WriteString("no closely matching meal yet")
		}
	}
	builder.WriteString("\nWant to swap one of the meals, or make the whole plan lighter or more filling? Just tell me.")
	if recommend.NeedDisclaimer {
		builder.WriteString(DisclaimerSuffix)
	}
	return builder.String()
}
