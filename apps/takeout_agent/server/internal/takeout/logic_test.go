package takeout

import (
	"encoding/json"
	"testing"
)

func TestSlotMergePrefersCurrentTurn(t *testing.T) {
	history := NewSlotBundle([]string{"Lunch"}, []string{"Tired"}, nil, []string{"Light"}, nil, nil, nil)
	current := NewSlotBundle([]string{"Dinner"}, nil, nil, []string{" High Protein "}, nil, nil, nil)
	merged := SlotMergeService{}.Merge(history, current)
	if len(merged.MealTime) != 1 || merged.MealTime[0] != "Dinner" {
		t.Fatalf("mealTime = %v, want [Dinner]", merged.MealTime)
	}
	if len(merged.Mood) != 1 || merged.Mood[0] != "Tired" {
		t.Fatalf("mood = %v, want history value kept", merged.Mood)
	}
	if len(merged.HealthGoal) != 1 || merged.HealthGoal[0] != "High Protein" {
		t.Fatalf("healthGoal = %v, want [High Protein] trimmed", merged.HealthGoal)
	}
}

func TestSlotBundleNormalizeDedupesAndTrims(t *testing.T) {
	bundle := NewSlotBundle([]string{" Lunch ", "Lunch", "", "Dinner"}, nil, nil, nil, nil, nil, nil)
	if len(bundle.MealTime) != 2 || bundle.MealTime[0] != "Lunch" || bundle.MealTime[1] != "Dinner" {
		t.Fatalf("mealTime = %v, want [Lunch Dinner]", bundle.MealTime)
	}
}

func TestClarifyRuleMissingSlots(t *testing.T) {
	rule := ClarifyRuleService{}
	if missing := rule.MissingSlots(EmptySlotBundle()); len(missing) != 2 {
		t.Fatalf("missing = %v, want mealTime and healthGoal", missing)
	}
	strong := NewSlotBundle([]string{"Lunch"}, nil, nil, nil, []string{"Sichuan"}, nil, nil)
	if missing := rule.MissingSlots(strong); len(missing) != 0 {
		t.Fatalf("missing = %v, want none with strong cuisine preference", missing)
	}
	onlyGoal := NewSlotBundle(nil, nil, nil, []string{"Light"}, nil, nil, nil)
	if missing := rule.MissingSlots(onlyGoal); len(missing) != 1 || missing[0] != "mealTime" {
		t.Fatalf("missing = %v, want [mealTime]", missing)
	}
}

func TestMealRankFiltersExcludesAndSorts(t *testing.T) {
	candidates := []MealItem{
		{ID: 1, Name: "Pork Knuckle", Slots: NewSlotBundle(nil, nil, nil, []string{"High Protein"}, nil, nil, nil)},
		{ID: 2, Name: "Chicken Breast", Slots: NewSlotBundle(nil, nil, nil, []string{"Light", "High Protein"}, nil, nil, nil)},
		{ID: 3, Name: "Excluded", Slots: NewSlotBundle(nil, nil, nil, []string{"Light", "High Protein"}, nil, nil, nil)},
	}
	query := NewSlotBundle(nil, nil, nil, []string{"Light", "High Protein"}, nil, nil, nil)
	ranked := MealRankService{}.Rank(candidates, query, []int64{3})
	if len(ranked) != 2 {
		t.Fatalf("ranked = %v, want 2 items after exclusion", ranked)
	}
	if ranked[0].ID != 2 {
		t.Fatalf("top = %d, want 2 (full healthGoal overlap)", ranked[0].ID)
	}
	wantTop := 1.0 / 7.0
	if ranked[0].MatchScore < wantTop-1e-9 || ranked[0].MatchScore > wantTop+1e-9 {
		t.Fatalf("top score = %v, want %v", ranked[0].MatchScore, wantTop)
	}
	if ranked[1].MatchScore >= ranked[0].MatchScore {
		t.Fatalf("scores not descending: %v then %v", ranked[0].MatchScore, ranked[1].MatchScore)
	}
}

func TestFallbackIntentPriority(t *testing.T) {
	cases := []struct {
		input string
		want  Intent
	}{
		{"", IntentClarifyNeeded},
		{"My stomach hurts, what can I eat", IntentHealthRisk},
		{"show me something else", IntentMealAdjust},
		{"plan my meals for the whole day", IntentMealPlan},
		{"who are you", IntentOther},
		{"what should i eat for dinner", IntentMealRecommendation},
		{"hmm", IntentClarifyNeeded},
	}
	for _, testCase := range cases {
		if got := fallbackIntent(testCase.input); got != testCase.want {
			t.Fatalf("fallbackIntent(%q) = %v, want %v", testCase.input, got, testCase.want)
		}
	}
}

func TestIntentReviseRules(t *testing.T) {
	revise := IntentReviseService{}
	base := SessionState{LastRecommendations: []int64{}}

	// Rule 1: health-risk keywords win even with another intent.
	got := revise.Revise(base, &IntentResult{Intent: IntentMealRecommendation, Confidence: 0.9}, "I have diabetes, is cake ok")
	if got.Intent != IntentHealthRisk {
		t.Fatalf("intent = %v, want HEALTH_RISK", got.Intent)
	}

	// Rule 2: adjust without history degrades to recommendation.
	got = revise.Revise(base, &IntentResult{Intent: IntentMealAdjust, Confidence: 0.9}, "something else")
	if got.Intent != IntentMealRecommendation {
		t.Fatalf("intent = %v, want MEAL_RECOMMENDATION", got.Intent)
	}

	// Rule 2 inverse: adjust with history stays.
	withHistory := SessionState{LastRecommendations: []int64{7}}
	got = revise.Revise(withHistory, &IntentResult{Intent: IntentMealAdjust, Confidence: 0.9}, "something else")
	if got.Intent != IntentMealAdjust {
		t.Fatalf("intent = %v, want MEAL_ADJUST", got.Intent)
	}

	// Rule 3: plan keywords force MEAL_PLAN.
	got = revise.Revise(base, &IntentResult{Intent: IntentMealRecommendation, Confidence: 0.9}, "I want a meal plan")
	if got.Intent != IntentMealPlan {
		t.Fatalf("intent = %v, want MEAL_PLAN", got.Intent)
	}

	// Rule 4: low-confidence recommendation degrades to clarify.
	got = revise.Revise(base, &IntentResult{Intent: IntentMealRecommendation, Confidence: 0.2}, "recommend dinner")
	if got.Intent != IntentClarifyNeeded {
		t.Fatalf("intent = %v, want CLARIFY_NEEDED", got.Intent)
	}

	// Nil result is handled conservatively.
	got = revise.Revise(base, nil, "recommend dinner")
	if got.Intent != IntentClarifyNeeded {
		t.Fatalf("intent = %v, want CLARIFY_NEEDED", got.Intent)
	}
}

func TestRiskGuardBlocksAndPasses(t *testing.T) {
	guard := RiskGuardService{}
	pass := guard.Check("dinner ideas", IntentMealRecommendation, TextOnlyResponse("Here are some light dishes."))
	if !pass.Passed || pass.RiskLevel != RiskLow {
		t.Fatalf("guard = %+v, want pass", pass)
	}
	blocked := guard.Check("can this cure my stomachache", IntentMealRecommendation, TextOnlyResponse("Sure, this will cure you."))
	if blocked.Passed || blocked.RiskLevel != RiskHigh {
		t.Fatalf("guard = %+v, want block", blocked)
	}
	if blocked.RewriteSuggestion != ConservativeMessage {
		t.Fatalf("rewrite = %q, want conservative message", blocked.RewriteSuggestion)
	}
	if len(blocked.BlockedReasons) == 0 {
		t.Fatal("blocked reasons should not be empty")
	}
	// Word-boundary matching: "curry" must not trigger the "cure" rule.
	curry := guard.Check("I want curry tonight", IntentMealRecommendation, TextOnlyResponse("A warm curry bowl works."))
	if !curry.Passed {
		t.Fatalf("guard = %+v, want pass for curry", curry)
	}
}

func TestExtractJSONObjectFromFencedOutput(t *testing.T) {
	content := "Sure!\n```json\n{\"intent\": \"MEAL_PLAN\", \"confidence\": 0.8}\n```\nHope that helps."
	root, err := ExtractJSONObject(content)
	if err != nil {
		t.Fatalf("ExtractJSONObject failed: %v", err)
	}
	if root["intent"] != "MEAL_PLAN" {
		t.Fatalf("intent = %v, want MEAL_PLAN", root["intent"])
	}
	if _, err := ExtractJSONObject("no json here"); err == nil {
		t.Fatal("expected error for non-JSON content")
	}
}

func TestPickSlotLabelsFiltersAndSplits(t *testing.T) {
	root := map[string]any{
		"healthGoal": "Fat Loss, High Protein, Miracle",
		"mealTime":   []any{"Lunch", "Brunch", "Second Breakfast"},
		"taste":      nil,
	}
	options := map[string][]string{
		"healthGoal": {"Fat Loss", "High Protein"},
		"mealTime":   {"Lunch", "Brunch"},
	}
	goals := PickSlotLabels(root, "healthGoal", options["healthGoal"])
	if len(goals) != 2 || goals[0] != "Fat Loss" || goals[1] != "High Protein" {
		t.Fatalf("healthGoal = %v, want [Fat Loss High Protein]", goals)
	}
	times := PickSlotLabels(root, "mealTime", options["mealTime"])
	if len(times) != 2 || times[0] != "Lunch" || times[1] != "Brunch" {
		t.Fatalf("mealTime = %v, want [Lunch Brunch]", times)
	}
	if got := PickSlotLabels(root, "taste", nil); len(got) != 0 {
		t.Fatalf("taste = %v, want empty", got)
	}
}

func TestResolveMealTimes(t *testing.T) {
	service := MealPlanService{}
	if got := service.ResolveMealTimes(EmptySlotBundle()); len(got) != 3 {
		t.Fatalf("empty slots = %v, want default three meals", got)
	}
	aggregate := NewSlotBundle([]string{"Three Meals"}, nil, nil, nil, nil, nil, nil)
	if got := service.ResolveMealTimes(aggregate); len(got) != 3 {
		t.Fatalf("aggregate = %v, want default three meals", got)
	}
	single := NewSlotBundle([]string{"Dinner"}, nil, nil, nil, nil, nil, nil)
	if got := service.ResolveMealTimes(single); len(got) != 3 {
		t.Fatalf("single = %v, want default three meals", got)
	}
	pair := NewSlotBundle([]string{"Brunch", "Dinner", "Brunch"}, nil, nil, nil, nil, nil, nil)
	got := service.ResolveMealTimes(pair)
	if len(got) != 2 || got[0] != "Brunch" || got[1] != "Dinner" {
		t.Fatalf("pair = %v, want [Brunch Dinner]", got)
	}
}

func TestSessionStateRoundTrip(t *testing.T) {
	state := FreshSessionState("sess_test", 42, SourcePersonal).
		WithIntent(IntentMealAdjust).
		WithPhase(PhaseRecommend).
		WithSlots(NewSlotBundle([]string{"Dinner"}, nil, nil, []string{"Light"}, nil, nil, nil)).
		AppendLastRecommendations([]int64{1, 2}).
		AppendLastRecommendations([]int64{2, 3})
	row, err := stateToRow(state)
	if err != nil {
		t.Fatalf("stateToRow failed: %v", err)
	}
	restored, err := stateFromRow(row, SourcePublic)
	if err != nil {
		t.Fatalf("stateFromRow failed: %v", err)
	}
	if restored.SourceMode != SourcePersonal {
		t.Fatalf("sourceMode = %v, want persisted PERSONAL over request PUBLIC", restored.SourceMode)
	}
	if restored.CurrentIntent != IntentMealAdjust || restored.Phase != PhaseRecommend {
		t.Fatalf("intent/phase = %v/%v, want MEAL_ADJUST/RECOMMEND", restored.CurrentIntent, restored.Phase)
	}
	if len(restored.LastRecommendations) != 3 {
		t.Fatalf("lastRecommendations = %v, want [1 2 3]", restored.LastRecommendations)
	}
	if len(restored.Slots.HealthGoal) != 1 || restored.Slots.HealthGoal[0] != "Light" {
		t.Fatalf("slots = %+v, want healthGoal [Light]", restored.Slots)
	}
}

func TestParseIntentResultFlatAndNested(t *testing.T) {
	options := map[string][]string{"mealTime": {"Lunch"}, "healthGoal": {"Light"}}
	nested := `{"intent": "MEAL_RECOMMENDATION", "slots": {"mealTime": "Lunch", "healthGoal": "Light"}, "confidence": 0.9}`
	result, err := parseIntentResult(nested, "lunch something light", options)
	if err != nil {
		t.Fatalf("parse nested failed: %v", err)
	}
	if result.Intent != IntentMealRecommendation || result.Confidence != 0.9 {
		t.Fatalf("result = %+v, want recommendation with 0.9", result)
	}
	if len(result.Slots.MealTime) != 1 || result.Slots.MealTime[0] != "Lunch" {
		t.Fatalf("slots = %+v, want mealTime [Lunch]", result.Slots)
	}
	flat := `{"intent": "UNKNOWN_INTENT", "mealTime": ["Lunch"]}`
	result, err = parseIntentResult(flat, "what should i eat for lunch", options)
	if err != nil {
		t.Fatalf("parse flat failed: %v", err)
	}
	if result.Intent != IntentMealRecommendation {
		t.Fatalf("intent = %v, want keyword fallback for unknown enum", result.Intent)
	}
	if len(result.Slots.MealTime) != 1 {
		t.Fatalf("slots = %+v, want flat mealTime picked", result.Slots)
	}
}

func TestEvaluationScoreHelpers(t *testing.T) {
	if got := costScore(int64Ptr(500)); got == nil || *got != 1.0 {
		t.Fatalf("costScore(500) = %v, want 1", got)
	}
	if got := costScore(int64Ptr(2000)); got == nil || *got != 0.5 {
		t.Fatalf("costScore(2000) = %v, want 0.5", got)
	}
	if got := latencyScore(int64Ptr(10000)); got == nil || *got != 0.0 {
		t.Fatalf("latencyScore(10000) = %v, want 0", got)
	}
	if got := weightedScore(floatPtr(1.0), nil, floatPtr(0.0)); got == nil || absDiff(*got, 0.6/0.9) > 1e-9 {
		t.Fatalf("weightedScore = %v, want 0.6/0.9", got)
	}
	if got := average([]*float64{nil, floatPtr(0.5), floatPtr(1.0)}); got == nil || *got != 0.75 {
		t.Fatalf("average = %v, want 0.75", got)
	}
	if got := average([]*float64{nil}); got != nil {
		t.Fatalf("average(all nil) = %v, want nil", got)
	}
	if got := toPercent(floatPtr(0.87654)); got == nil || *got != 87.65 {
		t.Fatalf("toPercent = %v, want 87.65", got)
	}
}

func TestParseTraceSnapshot(t *testing.T) {
	events := []map[string]any{
		{"eventType": "INTENT_REVISED", "outputPayload": `{"intent": "MEAL_ADJUST", "slots": {"mealTime": ["Dinner"]}}`},
		{"eventType": "ADJUST_CONTEXT_RESOLVED", "outputPayload": `{"excludeMealIds": [1, 2]}`},
		{"eventType": "AGENT_CALL", "totalTokens": float64(300)},
		{"eventType": "MEAL_RANKED", "outputPayload": `{"ranked": [{"id": 3}, {"id": 4}]}`},
		{"eventType": "RESPONSE_READY", "outputPayload": `{"speechText": "Here you go.", "displayBlocks": [{"id": 3}]}`},
	}
	traceJSON, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		t.Fatalf("marshal events failed: %v", err)
	}
	row := RequestTraceRow{Status: "SUCCESS", TraceJSON: string(traceJSON)}
	snapshot := parseTrace(row)
	if snapshot.intent != "MEAL_ADJUST" {
		t.Fatalf("intent = %v, want MEAL_ADJUST", snapshot.intent)
	}
	if snapshot.tokenCost == nil || *snapshot.tokenCost != 300 {
		t.Fatalf("tokenCost = %v, want 300", snapshot.tokenCost)
	}
	if !snapshot.hallucinationFree {
		t.Fatal("response card 3 is inside ranked set, want hallucinationFree")
	}
	if snapshot.multiTurnConsistency == nil || *snapshot.multiTurnConsistency != 1.0 {
		t.Fatalf("multiTurnConsistency = %v, want 1", snapshot.multiTurnConsistency)
	}
	if snapshot.finalText != "Here you go." {
		t.Fatalf("finalText = %q", snapshot.finalText)
	}
	if snapshot.fallbackUsed {
		t.Fatal("fallbackUsed should be false for a clean trace")
	}
}

func TestParseTracePlanSnapshotIsHallucinationFree(t *testing.T) {
	events := []map[string]any{
		{"eventType": "INTENT_REVISED", "outputPayload": `{"intent": "MEAL_PLAN", "slots": {"mealTime": ["Breakfast", "Dinner"]}}`},
		{"eventType": "MEAL_PLAN_SEARCHED", "outputPayload": `{"plannedCount": 2, "plannedMeals": [{"mealTime": "Breakfast", "matched": true, "mealId": 3}, {"mealTime": "Dinner", "matched": true, "mealId": 5}]}`},
		{"eventType": "RESPONSE_READY", "outputPayload": `{"speechText": "Here is your plan.", "displayBlocks": [{"id": 3}, {"id": 5}]}`},
	}
	traceJSON, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		t.Fatalf("marshal events failed: %v", err)
	}
	row := RequestTraceRow{Status: "SUCCESS", TraceJSON: string(traceJSON)}
	snapshot := parseTrace(row)
	if !snapshot.hallucinationFree {
		t.Fatal("planned meals 3 and 5 cover the response cards, want hallucinationFree")
	}
	if snapshot.recommendationCount != 2 {
		t.Fatalf("recommendationCount = %d, want 2", snapshot.recommendationCount)
	}
}

func TestParseTracePlanSnapshotFlagsInventedMeal(t *testing.T) {
	events := []map[string]any{
		{"eventType": "MEAL_PLAN_SEARCHED", "outputPayload": `{"plannedCount": 1, "plannedMeals": [{"mealTime": "Breakfast", "matched": true, "mealId": 3}, {"mealTime": "Dinner", "matched": false}]}`},
		{"eventType": "RESPONSE_READY", "outputPayload": `{"speechText": "Here is your plan.", "displayBlocks": [{"id": 3}, {"id": 99}]}`},
	}
	traceJSON, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		t.Fatalf("marshal events failed: %v", err)
	}
	row := RequestTraceRow{Status: "SUCCESS", TraceJSON: string(traceJSON)}
	if snapshot := parseTrace(row); snapshot.hallucinationFree {
		t.Fatal("card 99 is outside the planned set, want hallucinationFree = false")
	}
}

func TestSafetyCompliantConservativeMessage(t *testing.T) {
	if !safetyCompliant(ConservativeMessage) {
		t.Fatal("the fixed conservative refusal must be compliant by construction")
	}
	if !safetyCompliant("  " + ConservativeMessage + " ") {
		t.Fatal("surrounding whitespace must not change compliance")
	}
	if !safetyCompliant("Here are three light dinner ideas.") {
		t.Fatal("a normal reply should be compliant")
	}
	if safetyCompliant("This soup can cure your cold, guaranteed.") {
		t.Fatal("forbidden medical claims must stay non-compliant")
	}
	events := []map[string]any{
		{"eventType": "RESPONSE_READY", "outputPayload": jsonString(map[string]any{"speechText": ConservativeMessage, "displayBlocks": []any{}})},
	}
	traceJSON, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		t.Fatalf("marshal events failed: %v", err)
	}
	row := RequestTraceRow{Status: "SUCCESS", TraceJSON: string(traceJSON)}
	if snapshot := parseTrace(row); !snapshot.safetyCompliance {
		t.Fatal("HEALTH_RISK traces answering with the conservative message must score safetyCompliance = 1")
	}
}

func TestExpectedSlotsLabelSerializesEmptyArrays(t *testing.T) {
	normalized := NewSlotBundle(nil, nil, nil, nil, nil, nil, []string{"Quick"})
	value := jsonString(normalized)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatalf("unmarshal normalized bundle failed: %v", err)
	}
	for _, slotName := range SlotNames {
		raw, ok := decoded[slotName]
		if !ok {
			t.Fatalf("slot %s missing from %s", slotName, value)
		}
		if raw == nil {
			t.Fatalf("slot %s serialized as null, want []: %s", slotName, value)
		}
	}
}

func TestNeedsDisclaimer(t *testing.T) {
	if !NeedsDisclaimer(NewSlotBundle(nil, nil, nil, []string{"Fat Loss"}, nil, nil, nil)) {
		t.Fatal("Fat Loss should require the disclaimer")
	}
	if NeedsDisclaimer(NewSlotBundle(nil, nil, nil, []string{"Balanced"}, nil, nil, nil)) {
		t.Fatal("Balanced should not require the disclaimer")
	}
}

func TestPickSlotLabelsSplitsFullWidthSeparators(t *testing.T) {
	allowed := []string{"Fat Loss", "High Protein", "Light"}
	root := map[string]any{"healthGoal": "Fat Loss，High Protein、Light；Miracle"}
	got := PickSlotLabels(root, "healthGoal", allowed)
	if len(got) != 3 || got[0] != "Fat Loss" || got[1] != "High Protein" || got[2] != "Light" {
		t.Fatalf("healthGoal = %v, want [Fat Loss High Protein Light]", got)
	}
}

func TestParseSourceModeNormalizes(t *testing.T) {
	if mode, ok := ParseSourceMode(" personal "); !ok || mode != SourcePersonal {
		t.Fatalf("mode = %v (%v), want PERSONAL", mode, ok)
	}
	if _, ok := ParseSourceMode(""); ok {
		t.Fatal("empty sourceMode should not parse")
	}
	if _, ok := ParseSourceMode("PROTECTED"); ok {
		t.Fatal("unknown sourceMode should not parse")
	}
}

func TestSessionLocksArePrunedAfterRelease(t *testing.T) {
	orchestrator := &Orchestrator{sessionLocks: map[string]*sessionLock{}}

	first := orchestrator.acquireSessionLock("sess-1")
	first.mu.Lock()
	if len(orchestrator.sessionLocks) != 1 {
		t.Fatalf("locks = %d, want 1 while held", len(orchestrator.sessionLocks))
	}
	orchestrator.releaseSessionLock("sess-1", first)
	if len(orchestrator.sessionLocks) != 0 {
		t.Fatalf("locks = %d, want 0 after the only holder released", len(orchestrator.sessionLocks))
	}

	// A second acquirer keeps the entry alive until both release.
	holder := orchestrator.acquireSessionLock("sess-2")
	holder.mu.Lock()
	waiter := orchestrator.acquireSessionLock("sess-2")
	orchestrator.releaseSessionLock("sess-2", holder)
	if len(orchestrator.sessionLocks) != 1 {
		t.Fatalf("locks = %d, want 1 while a waiter remains", len(orchestrator.sessionLocks))
	}
	waiter.mu.Lock()
	orchestrator.releaseSessionLock("sess-2", waiter)
	if len(orchestrator.sessionLocks) != 0 {
		t.Fatalf("locks = %d, want 0 after every holder released", len(orchestrator.sessionLocks))
	}
}

func TestNotFoundErrorCarriesStatus(t *testing.T) {
	err := NewNotFoundError("trace not found: %s", "trace_1")
	if err.Status != 404 {
		t.Fatalf("status = %d, want 404", err.Status)
	}
	if plain := NewError("bad request"); plain.Status != 0 {
		t.Fatalf("status = %d, want 0 (default 400)", plain.Status)
	}
	if _, ok := AsError(err); !ok {
		t.Fatal("AsError should recognize NewNotFoundError")
	}
}

func int64Ptr(value int64) *int64 { return &value }

func absDiff(left, right float64) float64 {
	if left > right {
		return left - right
	}
	return right - left
}

func TestPickSlotLabelsCaseInsensitiveCanonicalizes(t *testing.T) {
	root := map[string]any{
		"healthGoal": "fat loss, HIGH PROTEIN",
		"mealTime":   []any{"lunch"},
	}
	options := map[string][]string{
		"healthGoal": {"Fat Loss", "High Protein"},
		"mealTime":   {"Lunch", "Brunch"},
	}
	goals := PickSlotLabels(root, "healthGoal", options["healthGoal"])
	if len(goals) != 2 || goals[0] != "Fat Loss" || goals[1] != "High Protein" {
		t.Fatalf("healthGoal = %v, want canonical [Fat Loss High Protein]", goals)
	}
	times := PickSlotLabels(root, "mealTime", options["mealTime"])
	if len(times) != 1 || times[0] != "Lunch" {
		t.Fatalf("mealTime = %v, want canonical [Lunch]", times)
	}
}

func TestParseIntentResultCoercesStringConfidence(t *testing.T) {
	result, err := parseIntentResult(
		`{"intent":"MEAL_RECOMMENDATION","slots":{"mealTime":"Lunch"},"confidence":"0.9"}`,
		"lunch please",
		map[string][]string{"mealTime": {"Lunch"}},
	)
	if err != nil {
		t.Fatalf("parseIntentResult() error = %v", err)
	}
	if result.Confidence != 0.9 {
		t.Fatalf("confidence = %v, want 0.9 from numeric string", result.Confidence)
	}
	invalid, err := parseIntentResult(
		`{"intent":"OTHER","confidence":"high"}`,
		"hello",
		nil,
	)
	if err != nil {
		t.Fatalf("parseIntentResult() error = %v", err)
	}
	if invalid.Confidence != 0.5 {
		t.Fatalf("confidence = %v, want 0.5 default for non-numeric string", invalid.Confidence)
	}
}

func TestParseRecommendOutputCoercesScalars(t *testing.T) {
	topMeals := []MealItem{{ID: 7, Name: "Wontons", Slots: EmptySlotBundle()}}
	content := `{"recommendations":[{"mealId":"7","reason":123}],"speechText":42}`
	options, speech := parseRecommendOutput(content, topMeals, EmptySlotBundle())
	if len(options) != 1 || options[0].Reason != "123" {
		t.Fatalf("options = %+v, want numeric reason coerced to text", options)
	}
	if speech != "42" {
		t.Fatalf("speechText = %q, want numeric value coerced to text", speech)
	}
}

func TestHealthRiskKeywordsCoverExtremeTakeoutPhrases(t *testing.T) {
	for _, input := range []string{
		"I will eat nothing for a week",
		"can I only drink water to slim down",
		"only water for three days, fine?",
	} {
		if got := fallbackIntent(input); got != IntentHealthRisk {
			t.Fatalf("fallbackIntent(%q) = %s, want HEALTH_RISK", input, got)
		}
		revised := IntentReviseService{}.Revise(
			SessionState{},
			&IntentResult{Intent: IntentMealRecommendation, Slots: EmptySlotBundle(), Confidence: 0.9},
			input,
		)
		if revised.Intent != IntentHealthRisk {
			t.Fatalf("Revise(%q) = %s, want HEALTH_RISK", input, revised.Intent)
		}
	}
}

func TestJsonScalarCoercionHelpers(t *testing.T) {
	if value, ok := jsonFloat("0.25"); !ok || value != 0.25 {
		t.Fatalf("jsonFloat(string) = %v, %v; want 0.25, true", value, ok)
	}
	if _, ok := jsonFloat("abc"); ok {
		t.Fatal("jsonFloat(abc) should fail")
	}
	if value, ok := jsonInt64("42"); !ok || value != 42 {
		t.Fatalf("jsonInt64(string) = %v, %v; want 42, true", value, ok)
	}
	if value, ok := jsonInt64(float64(9)); !ok || value != 9 {
		t.Fatalf("jsonInt64(float) = %v, %v; want 9, true", value, ok)
	}
	if got := jsonText(true); got != "true" {
		t.Fatalf("jsonText(bool) = %q, want true", got)
	}
	if got := jsonText(map[string]any{}); got != "" {
		t.Fatalf("jsonText(map) = %q, want empty", got)
	}
	if got := jsonText(nil); got != "" {
		t.Fatalf("jsonText(nil) = %q, want empty", got)
	}
}
