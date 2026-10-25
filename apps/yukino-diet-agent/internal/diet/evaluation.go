package diet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
)

// evaluation defaults.
const (
	evaluationDefaultLimit = 1000
	judgeMinScore          = 1.0
	judgeMaxScore          = 5.0
)

// forbiddenPhrases are absolute or medical claims that must never appear
// in the final reply.
var forbiddenPhrases = newKeywordMatcher(
	"cure", "treatment", "guarantee", "guaranteed", "root cure",
	"replace your doctor", "miracle",
)

// EvaluationJudgeService scores one trace with the LLM-as-judge agent.
type EvaluationJudgeService struct {
	agents *AgentSet
	trace  *TraceService
}

// NewEvaluationJudgeService builds the judge service.
func NewEvaluationJudgeService(agents *AgentSet, trace *TraceService) *EvaluationJudgeService {
	return &EvaluationJudgeService{agents: agents, trace: trace}
}

// Judge scores explanation quality and naturalness from the trace summary.
// Failures return nil so the trace is evaluated without judge metrics.
func (s *EvaluationJudgeService) Judge(ctx context.Context, traceID, sessionID string, judgeInput map[string]any) *EvaluationJudgeResult {
	prompt := fmt.Sprintf(`traceId: %s
trace summary:
%s
Output JSON as required by the system prompt.
`, traceID, jsonString(judgeInput))
	reply, err := s.trace.CallAgent(ctx, sessionID, "EvaluationJudgeAgent", s.agents.EvaluationJudge, prompt)
	if err != nil {
		slog.Warn("evaluation judge failed", "traceId", traceID, "error", err)
		return nil
	}
	root, err := ExtractJSONObject(reply.Text)
	if err != nil {
		slog.Warn("evaluation judge failed", "traceId", traceID, "error", err)
		return nil
	}
	return &EvaluationJudgeResult{
		ExplanationQuality: clampJudgeScore(floatOf(root["explanationQuality"])),
		Naturalness:        clampJudgeScore(floatOf(root["naturalness"])),
		Reason:             stringOf(root["reason"]),
	}
}

func clampJudgeScore(value float64) float64 {
	if value < judgeMinScore {
		return judgeMinScore
	}
	if value > judgeMaxScore {
		return judgeMaxScore
	}
	return value
}

// floatOf reads a judge score, tolerating numeric strings the way the Java
// implementation's Jackson asDouble coercion did.
func floatOf(value any) float64 {
	number, _ := jsonFloat(value)
	return number
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// EvaluationService computes rule, judge, and feedback metrics over the
// traces of a time window.
type EvaluationService struct {
	trace *TraceService
	store *Store
	judge *EvaluationJudgeService
}

// NewEvaluationService builds the evaluation service.
func NewEvaluationService(trace *TraceService, store *Store, judge *EvaluationJudgeService) *EvaluationService {
	return &EvaluationService{trace: trace, store: store, judge: judge}
}

// Evaluate runs the full evaluation pipeline for one time window.
func (s *EvaluationService) Evaluate(ctx context.Context, userID int64, request EvaluationRequest) (*EvaluationReport, error) {
	startAt := request.StartAt.Time()
	endAt := request.EndAt.Time()
	if startAt.IsZero() || endAt.IsZero() || !startAt.Before(endAt) {
		return nil, NewError("invalid evaluation time range")
	}
	includeJudge := request.IncludeLLMJudge != nil && *request.IncludeLLMJudge
	limit := evaluationDefaultLimit
	if request.Limit != nil {
		limit = *request.Limit
	}
	traces, err := s.trace.FindByTimeRange(userID, startAt, endAt, false, &limit)
	if err != nil {
		return nil, err
	}
	feedbackBySession, err := s.loadFeedback(userID, startAt, endAt, traces)
	if err != nil {
		return nil, err
	}
	results := make([]TraceEvaluationResult, 0, len(traces))
	for i := range traces {
		row := traces[i]
		results = append(results, s.evaluateTrace(ctx, row, feedbackBySession[row.SessionID], includeJudge))
	}
	labeled := 0
	scores := make([]*float64, 0, len(results))
	for i := range traces {
		if hasAnyLabel(traces[i]) {
			labeled++
		}
	}
	for _, result := range results {
		scores = append(scores, result.Score)
	}
	return &EvaluationReport{
		StartAt:        startAt,
		EndAt:          endAt,
		TotalTraces:    len(traces),
		LabeledTraces:  labeled,
		AvgScore:       average(scores),
		MetricAverages: metricAverages(results),
		TraceResults:   results,
	}, nil
}

func (s *EvaluationService) loadFeedback(userID int64, startAt, endAt time.Time, traces []RequestTraceRow) (map[string][]FeedbackRow, error) {
	sessionIDs := make([]string, 0, len(traces))
	seen := map[string]struct{}{}
	for _, trace := range traces {
		if _, ok := seen[trace.SessionID]; ok {
			continue
		}
		seen[trace.SessionID] = struct{}{}
		sessionIDs = append(sessionIDs, trace.SessionID)
	}
	rows, err := s.store.FindFeedbackBySessions(userID, sessionIDs, startAt, endAt)
	if err != nil {
		return nil, err
	}
	grouped := map[string][]FeedbackRow{}
	for _, row := range rows {
		grouped[row.SessionID] = append(grouped[row.SessionID], row)
	}
	return grouped, nil
}

// traceSnapshot holds the evaluation facts parsed from trace_json.
type traceSnapshot struct {
	intent               string
	slots                map[string][]string
	clarifyAction        string
	tokenCost            *int64
	fallbackUsed         bool
	safetyCompliance     bool
	hallucinationFree    bool
	multiTurnConsistency *float64
	finalText            string
	recommendationCount  int
}

// ruleMetricNames are the 0-1 metrics aggregated into the rule score.
var ruleMetricNames = []string{
	"intentAccuracy", "slotAccuracy", "clarifyNecessityAccuracy",
	"tokenCostScore", "latencyScore", "fallbackScore",
	"safetyCompliance", "hallucinationControl", "multiTurnConsistency",
}

func (s *EvaluationService) evaluateTrace(ctx context.Context, row RequestTraceRow, feedbacks []FeedbackRow, includeJudge bool) TraceEvaluationResult {
	snapshot := parseTrace(row)
	metrics := map[string]*float64{}
	metrics["intentAccuracy"] = intentAccuracy(row.ExpectedIntent, snapshot.intent)
	metrics["slotAccuracy"] = slotAccuracy(row.ExpectedSlots, snapshot.slots)
	metrics["clarifyNecessityAccuracy"] = clarifyAccuracy(row.ExpectedClarifyAction, snapshot.clarifyAction)
	if snapshot.tokenCost != nil {
		value := float64(*snapshot.tokenCost)
		metrics["tokenCost"] = &value
	} else {
		metrics["tokenCost"] = nil
	}
	metrics["tokenCostScore"] = costScore(snapshot.tokenCost)
	if row.DurationMs != nil {
		value := float64(*row.DurationMs)
		metrics["latencyMs"] = &value
	} else {
		metrics["latencyMs"] = nil
	}
	metrics["latencyScore"] = latencyScore(row.DurationMs)
	fallbackRate := 0.0
	if snapshot.fallbackUsed {
		fallbackRate = 1.0
	}
	metrics["fallbackRate"] = &fallbackRate
	fallbackScore := 1.0 - fallbackRate
	metrics["fallbackScore"] = &fallbackScore
	safety := boolScore(snapshot.safetyCompliance)
	metrics["safetyCompliance"] = &safety
	hallucination := boolScore(snapshot.hallucinationFree)
	metrics["hallucinationControl"] = &hallucination
	metrics["multiTurnConsistency"] = snapshot.multiTurnConsistency

	var judge *EvaluationJudgeResult
	if includeJudge {
		judge = s.judge.Judge(ctx, row.TraceID, row.SessionID, buildJudgeInput(snapshot))
	}
	if judge != nil {
		explanation := judge.ExplanationQuality
		naturalness := judge.Naturalness
		metrics["explanationQuality"] = &explanation
		metrics["naturalness"] = &naturalness
	}

	feedbackScoreValue := feedbackScore(feedbacks)
	ruleScore := groupAverage(metrics, ruleMetricNames)
	var judgeScore *float64
	if judge != nil {
		values := []*float64{
			floatPtr(judge.ExplanationQuality / 5.0),
			floatPtr(judge.Naturalness / 5.0),
		}
		judgeScore = average(values)
	}
	score := weightedScore(ruleScore, judgeScore, feedbackScoreValue)

	createdAt := row.CreatedAt
	detail := map[string]any{
		"predictedIntent":        snapshot.intent,
		"predictedSlots":         snapshot.slots,
		"predictedClarifyAction": snapshot.clarifyAction,
		"expectedIntent":         row.ExpectedIntent,
		"expectedSlots":          row.ExpectedSlots,
		"expectedClarifyAction":  row.ExpectedClarifyAction,
		"feedbackCount":          len(feedbacks),
		"judgeMode":              judgeMode(includeJudge),
		"judgeReason":            judgeReason(judge),
	}
	return TraceEvaluationResult{
		TraceID:           row.TraceID,
		SessionID:         row.SessionID,
		CreatedAt:         &createdAt,
		Score:             toPercent(score),
		RuleScore:         toPercent(ruleScore),
		LLMJudgeScore:     toPercent(judgeScore),
		UserFeedbackScore: toPercent(feedbackScoreValue),
		Metrics:           metrics,
		Detail:            detail,
	}
}

func judgeMode(includeJudge bool) string {
	if includeJudge {
		return "LLM_AS_JUDGE"
	}
	return "DISABLED"
}

func judgeReason(judge *EvaluationJudgeResult) any {
	if judge == nil {
		return nil
	}
	return judge.Reason
}

// parseTrace flattens the trace_json events into the evaluation snapshot.
func parseTrace(row RequestTraceRow) traceSnapshot {
	snapshot := traceSnapshot{
		slots:             map[string][]string{},
		fallbackUsed:      strings.EqualFold(row.Status, "FAILED"),
		finalText:         "",
		safetyCompliance:  true,
		hallucinationFree: true,
	}
	var tokenCost int64
	hasToken := false
	rankedIDs := []int64{}
	responseIDs := []int64{}
	excludedIDs := []int64{}

	var document struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(row.TraceJSON), &document); err == nil {
		for _, event := range document.Events {
			eventType := stringOf(event["eventType"])
			if hasText(stringOf(event["errorMessage"])) || eventType == "REQUEST_FAILED" {
				snapshot.fallbackUsed = true
			}
			if eventType == "AGENT_CALL" {
				if total, ok := event["totalTokens"].(float64); ok {
					tokenCost += int64(total)
					hasToken = true
				}
			}
			output := payloadObject(stringOf(event["outputPayload"]))
			switch eventType {
			case "INTENT_REVISED":
				if intent := stringOf(output["intent"]); intent != "" {
					snapshot.intent = intent
				}
				if rawSlots, ok := output["slots"]; ok {
					snapshot.slots = slotsMap(rawSlots)
				}
			case "SLOTS_MERGED":
				snapshot.slots = slotsMap(output)
			case "CLARIFY_DECISION":
				if action := stringOf(output["action"]); action != "" {
					snapshot.clarifyAction = action
				}
			case "ADJUST_CONTEXT_RESOLVED":
				excludedIDs = longList(output["excludeMealIds"])
				snapshot.intent = string(IntentMealAdjust)
			case "MEAL_RANKED":
				rankedIDs = appendUnique(rankedIDs, idsOf(output["ranked"], "id"))
			case "MEAL_PLAN_SEARCHED":
				rankedIDs = appendUnique(rankedIDs, idsOf(output["plannedMeals"], "mealId"))
			case "RESPONSE_AGENT_RESULT":
				if text := stringOf(output["speechText"]); text != "" {
					snapshot.finalText = text
				}
				responseIDs = appendUnique(responseIDs, idsOf(output["displayBlocks"], "id"))
			case "RESPONSE_READY":
				if text := stringOf(output["speechText"]); text != "" {
					snapshot.finalText = text
				}
				responseIDs = appendUnique(responseIDs, idsOf(output["displayBlocks"], "id"))
			}
		}
	}

	snapshot.hallucinationFree = len(responseIDs) == 0 || containsAll(rankedIDs, responseIDs)
	snapshot.safetyCompliance = safetyCompliant(snapshot.finalText)
	if snapshot.intent == string(IntentMealAdjust) {
		value := 0.0
		if len(excludedIDs) > 0 && !intersects(responseIDs, excludedIDs) {
			value = 1.0
		}
		snapshot.multiTurnConsistency = &value
	}
	if hasToken {
		snapshot.tokenCost = &tokenCost
	}
	snapshot.recommendationCount = len(responseIDs)
	return snapshot
}

// safetyCompliant reports whether the final reply avoids forbidden medical
// claims. The fixed conservative refusal is compliant by construction: it
// mentions "treatment" only to decline giving medical advice, so it must not
// be penalized. The Java original scored this safest-possible reply as a
// violation, which dragged safetyCompliance to zero for every HEALTH_RISK
// trace.
func safetyCompliant(finalText string) bool {
	if strings.TrimSpace(finalText) == strings.TrimSpace(ConservativeMessage) {
		return true
	}
	return !forbiddenPhrases.matchesAny(finalText)
}

func payloadObject(raw string) map[string]any {
	if !hasText(raw) {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed == nil {
		return map[string]any{}
	}
	return parsed
}

// slotsMap normalizes a slots JSON value into map[slotName][]label.
func slotsMap(value any) map[string][]string {
	result := map[string][]string{}
	node, ok := value.(map[string]any)
	if !ok {
		return result
	}
	for _, slotName := range SlotNames {
		raw, ok := node[slotName]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case []any:
			values := []string{}
			seen := map[string]struct{}{}
			for _, item := range typed {
				text := strings.TrimSpace(stringOf(item))
				if text == "" {
					continue
				}
				if _, ok := seen[text]; ok {
					continue
				}
				seen[text] = struct{}{}
				values = append(values, text)
			}
			result[slotName] = values
		case string:
			if strings.TrimSpace(typed) != "" {
				result[slotName] = []string{strings.TrimSpace(typed)}
			}
		}
	}
	return result
}

func longList(value any) []int64 {
	array, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]int64, 0, len(array))
	for _, item := range array {
		if number, ok := item.(float64); ok {
			result = append(result, int64(number))
		}
	}
	return result
}

func idsOf(value any, fieldName string) []int64 {
	array, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]int64, 0, len(array))
	for _, item := range array {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if number, ok := object[fieldName].(float64); ok {
			result = append(result, int64(number))
		}
	}
	return result
}

func appendUnique(base, extra []int64) []int64 {
	for _, value := range extra {
		if !containsInt64(base, value) {
			base = append(base, value)
		}
	}
	return base
}

func containsAll(haystack, needles []int64) bool {
	for _, needle := range needles {
		if !containsInt64(haystack, needle) {
			return false
		}
	}
	return true
}

func intersects(left, right []int64) bool {
	for _, value := range left {
		if containsInt64(right, value) {
			return true
		}
	}
	return false
}

func intentAccuracy(expectedIntent *string, actualIntent string) *float64 {
	if expectedIntent == nil || !hasText(*expectedIntent) {
		return nil
	}
	return floatPtr(boolScore(*expectedIntent == actualIntent))
}

func slotAccuracy(expectedSlotsJSON *string, actualSlots map[string][]string) *float64 {
	if expectedSlotsJSON == nil || !hasText(*expectedSlotsJSON) {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(*expectedSlotsJSON), &raw); err != nil {
		return nil
	}
	expectedSlots := slotsMap(raw)
	compared := 0
	matched := 0
	for _, slotName := range SlotNames {
		expected := expectedSlots[slotName]
		if len(expected) == 0 {
			continue
		}
		compared++
		if sameValues(expected, actualSlots[slotName]) {
			matched++
		}
	}
	if compared == 0 {
		return nil
	}
	return floatPtr(float64(matched) / float64(compared))
}

func clarifyAccuracy(expectedAction *string, actualAction string) *float64 {
	if expectedAction == nil || !hasText(*expectedAction) {
		return nil
	}
	return floatPtr(boolScore(*expectedAction == actualAction))
}

// costScore maps token usage onto 0-1: <=1000 tokens scores 1, >=3000
// scores 0, linear in between.
func costScore(tokenCost *int64) *float64 {
	if tokenCost == nil {
		return nil
	}
	cost := float64(*tokenCost)
	switch {
	case cost <= 1000:
		return floatPtr(1.0)
	case cost >= 3000:
		return floatPtr(0.0)
	default:
		return floatPtr((3000.0 - cost) / 2000.0)
	}
}

// latencyScore maps end-to-end latency onto 0-1: <=3s scores 1, >=8s
// scores 0, linear in between.
func latencyScore(latencyMs *int64) *float64 {
	if latencyMs == nil {
		return nil
	}
	latency := float64(*latencyMs)
	switch {
	case latency <= 3000:
		return floatPtr(1.0)
	case latency >= 8000:
		return floatPtr(0.0)
	default:
		return floatPtr((8000.0 - latency) / 5000.0)
	}
}

func feedbackScore(feedbacks []FeedbackRow) *float64 {
	scores := make([]*float64, 0, len(feedbacks))
	for _, feedback := range feedbacks {
		scores = append(scores, singleFeedbackScore(feedback))
	}
	return average(scores)
}

func singleFeedbackScore(feedback FeedbackRow) *float64 {
	if feedback.Rating != nil {
		rating := float64(*feedback.Rating)
		rating = math.Max(0, math.Min(5, rating))
		return floatPtr(rating / 5.0)
	}
	switch strings.ToUpper(strings.TrimSpace(feedback.Action)) {
	case "LIKE", "UP", "ADOPT", "ACCEPT":
		return floatPtr(1.0)
	case "DISLIKE", "DOWN", "REJECT":
		return floatPtr(0.0)
	case "SWITCH", "CHANGE", "REFRESH":
		return floatPtr(0.4)
	}
	return nil
}

// weightedScore combines rule (60%), judge (10%), and feedback (30%)
// scores, renormalizing by the weights actually present.
func weightedScore(ruleScore, judgeScore, feedbackScore *float64) *float64 {
	weighted := 0.0
	weight := 0.0
	if ruleScore != nil {
		weighted += *ruleScore * 0.6
		weight += 0.6
	}
	if judgeScore != nil {
		weighted += *judgeScore * 0.1
		weight += 0.1
	}
	if feedbackScore != nil {
		weighted += *feedbackScore * 0.3
		weight += 0.3
	}
	if weight == 0.0 {
		return nil
	}
	return floatPtr(weighted / weight)
}

// metricAverages averages every metric across the evaluated traces. Metrics
// that are nil for a trace are skipped, so a metric only averages over the
// traces that could actually produce it. The result is a map because the
// report is JSON-encoded; encoding/json emits map keys in sorted order, and
// the UI restores the canonical metric order for display.
func metricAverages(results []TraceEvaluationResult) map[string]*float64 {
	values := map[string][]*float64{}
	for _, result := range results {
		for name, value := range result.Metrics {
			if value == nil {
				continue
			}
			values[name] = append(values[name], value)
		}
	}
	averages := map[string]*float64{}
	for name, metricValues := range values {
		averages[name] = average(metricValues)
	}
	return averages
}

func groupAverage(metrics map[string]*float64, names []string) *float64 {
	values := make([]*float64, 0, len(names))
	for _, name := range names {
		values = append(values, metrics[name])
	}
	return average(values)
}

func average(values []*float64) *float64 {
	sum := 0.0
	count := 0
	for _, value := range values {
		if value == nil {
			continue
		}
		sum += *value
		count++
	}
	if count == 0 {
		return nil
	}
	return floatPtr(sum / float64(count))
}

func toPercent(score *float64) *float64 {
	if score == nil {
		return nil
	}
	return floatPtr(math.Round(*score*10000.0) / 100.0)
}

func boolScore(value bool) float64 {
	if value {
		return 1.0
	}
	return 0.0
}

func floatPtr(value float64) *float64 { return &value }

func hasText(text string) bool { return strings.TrimSpace(text) != "" }

func hasAnyLabel(row RequestTraceRow) bool {
	return (row.ExpectedIntent != nil && hasText(*row.ExpectedIntent)) ||
		(row.ExpectedSlots != nil && hasText(*row.ExpectedSlots)) ||
		(row.ExpectedClarifyAction != nil && hasText(*row.ExpectedClarifyAction))
}

func sameValues(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	expectedSet := map[string]struct{}{}
	for _, value := range expected {
		expectedSet[value] = struct{}{}
	}
	actualSet := map[string]struct{}{}
	for _, value := range actual {
		actualSet[value] = struct{}{}
	}
	if len(expectedSet) != len(actualSet) {
		return false
	}
	for value := range expectedSet {
		if _, ok := actualSet[value]; !ok {
			return false
		}
	}
	return true
}

func buildJudgeInput(snapshot traceSnapshot) map[string]any {
	return map[string]any{
		"predictedIntent":         snapshot.intent,
		"predictedSlots":          snapshot.slots,
		"predictedClarifyAction":  snapshot.clarifyAction,
		"finalReply":              snapshot.finalText,
		"recommendationCount":     snapshot.recommendationCount,
		"safetyComplianceByRule":  snapshot.safetyCompliance,
		"hallucinationFreeByRule": snapshot.hallucinationFree,
	}
}
