// Package takeout contains the domain model and business services of the takeout
// recommendation agent, migrated from the Java Spring Boot implementation.
package takeout

import (
	"encoding/json"
	"strings"
	"time"
)

// Intent classifies one user turn; it drives the orchestrator state machine.
type Intent string

// Supported intents.
const (
	IntentMealRecommendation Intent = "MEAL_RECOMMENDATION"
	IntentClarifyNeeded      Intent = "CLARIFY_NEEDED"
	IntentMealAdjust         Intent = "MEAL_ADJUST"
	IntentMealPlan           Intent = "MEAL_PLAN"
	IntentHealthRisk         Intent = "HEALTH_RISK"
	IntentOther              Intent = "OTHER"
)

// ParseIntent converts a raw string into an Intent, reporting whether the
// value is a known intent name.
func ParseIntent(raw string) (Intent, bool) {
	switch Intent(raw) {
	case IntentMealRecommendation, IntentClarifyNeeded, IntentMealAdjust,
		IntentMealPlan, IntentHealthRisk, IntentOther:
		return Intent(raw), true
	}
	return "", false
}

// SessionPhase is the conversation stage tracked on each session.
type SessionPhase string

// Supported session phases.
const (
	PhaseStart     SessionPhase = "START"
	PhaseClarify   SessionPhase = "CLARIFY"
	PhaseRecommend SessionPhase = "RECOMMEND"
	PhasePlan      SessionPhase = "PLAN"
)

// ParsePhase converts a raw string into a SessionPhase, falling back to
// PhaseStart for missing or dirty values.
func ParsePhase(raw string) SessionPhase {
	switch SessionPhase(raw) {
	case PhaseStart, PhaseClarify, PhaseRecommend, PhasePlan:
		return SessionPhase(raw)
	}
	return PhaseStart
}

// SourceMode selects the meal data source; PERSONAL and PUBLIC are never
// mixed inside one session.
type SourceMode string

// Supported source modes.
const (
	SourcePersonal SourceMode = "PERSONAL"
	SourcePublic   SourceMode = "PUBLIC"
)

// ParseSourceMode converts a raw string into a SourceMode.
func ParseSourceMode(raw string) (SourceMode, bool) {
	switch SourceMode(strings.ToUpper(strings.TrimSpace(raw))) {
	case SourcePersonal:
		return SourcePersonal, true
	case SourcePublic:
		return SourcePublic, true
	}
	return "", false
}

// ClarifyAction is the clarify node decision: ask the user for more details
// or continue into the recommendation pipeline.
type ClarifyAction string

// Supported clarify actions.
const (
	ClarifyAsk   ClarifyAction = "ASK"
	ClarifyReady ClarifyAction = "READY"
)

// ParseClarifyAction converts a raw string into a ClarifyAction.
func ParseClarifyAction(raw string) (ClarifyAction, bool) {
	switch ClarifyAction(strings.ToUpper(strings.TrimSpace(raw))) {
	case ClarifyAsk:
		return ClarifyAsk, true
	case ClarifyReady:
		return ClarifyReady, true
	}
	return "", false
}

// RiskLevel grades the health-risk guard outcome.
type RiskLevel string

// Supported risk levels.
const (
	RiskLow  RiskLevel = "LOW"
	RiskHigh RiskLevel = "HIGH"
)

// UserIDHeader carries the caller identity on every API request, mirroring
// the Java X-User-Id header convention.
const UserIDHeader = "X-User-Id"

// SlotNames lists the seven canonical slot dimensions in stable order.
var SlotNames = []string{"mealTime", "mood", "scene", "healthGoal", "cuisine", "taste", "convenience"}

// SlotBundle carries the seven normalized slot dimensions. Every field is
// non-nil after construction; unknown labels are filtered by callers.
type SlotBundle struct {
	MealTime    []string `json:"mealTime"`
	Mood        []string `json:"mood"`
	Scene       []string `json:"scene"`
	HealthGoal  []string `json:"healthGoal"`
	Cuisine     []string `json:"cuisine"`
	Taste       []string `json:"taste"`
	Convenience []string `json:"convenience"`
}

// NewSlotBundle builds a SlotBundle, normalizing every dimension.
func NewSlotBundle(mealTime, mood, scene, healthGoal, cuisine, taste, convenience []string) SlotBundle {
	return SlotBundle{
		MealTime:    normalizeLabels(mealTime),
		Mood:        normalizeLabels(mood),
		Scene:       normalizeLabels(scene),
		HealthGoal:  normalizeLabels(healthGoal),
		Cuisine:     normalizeLabels(cuisine),
		Taste:       normalizeLabels(taste),
		Convenience: normalizeLabels(convenience),
	}
}

// EmptySlotBundle returns a bundle with all seven dimensions empty.
func EmptySlotBundle() SlotBundle {
	return NewSlotBundle(nil, nil, nil, nil, nil, nil, nil)
}

// IsEmpty reports whether every dimension is empty.
func (b SlotBundle) IsEmpty() bool {
	return len(b.MealTime) == 0 && len(b.Mood) == 0 && len(b.Scene) == 0 &&
		len(b.HealthGoal) == 0 && len(b.Cuisine) == 0 && len(b.Taste) == 0 &&
		len(b.Convenience) == 0
}

// Get returns the values of a slot dimension by name.
func (b SlotBundle) Get(name string) []string {
	switch name {
	case "mealTime":
		return b.MealTime
	case "mood":
		return b.Mood
	case "scene":
		return b.Scene
	case "healthGoal":
		return b.HealthGoal
	case "cuisine":
		return b.Cuisine
	case "taste":
		return b.Taste
	case "convenience":
		return b.Convenience
	}
	return nil
}

// WithMealTime returns a copy of the bundle with mealTime replaced.
func (b SlotBundle) WithMealTime(mealTime []string) SlotBundle {
	next := b
	next.MealTime = normalizeLabels(mealTime)
	return next
}

// normalizeLabels trims, drops blank entries, and de-duplicates labels while
// preserving order.
func normalizeLabels(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// SessionState is the persisted conversation state of one session. Values
// are updated immutably via the With* helpers.
type SessionState struct {
	SessionID           string       `json:"sessionId"`
	UserID              int64        `json:"userId"`
	Phase               SessionPhase `json:"phase"`
	SourceMode          SourceMode   `json:"sourceMode"`
	CurrentIntent       Intent       `json:"currentIntent,omitempty"`
	Slots               SlotBundle   `json:"slots"`
	LastRecommendations []int64      `json:"lastRecommendations"`
}

// FreshSessionState builds the initial state for a new session.
func FreshSessionState(sessionID string, userID int64, sourceMode SourceMode) SessionState {
	return SessionState{
		SessionID:           sessionID,
		UserID:              userID,
		Phase:               PhaseStart,
		SourceMode:          sourceMode,
		Slots:               EmptySlotBundle(),
		LastRecommendations: []int64{},
	}
}

// WithPhase returns a copy with an updated phase.
func (s SessionState) WithPhase(phase SessionPhase) SessionState {
	s.Phase = phase
	return s
}

// WithIntent returns a copy with an updated current intent.
func (s SessionState) WithIntent(intent Intent) SessionState {
	s.CurrentIntent = intent
	return s
}

// WithSlots returns a copy with updated slots.
func (s SessionState) WithSlots(slots SlotBundle) SessionState {
	s.Slots = slots
	return s
}

// WithSourceMode returns a copy bound to a different source mode.
func (s SessionState) WithSourceMode(mode SourceMode) SessionState {
	s.SourceMode = mode
	return s
}

// AppendLastRecommendations accumulates recommended meal IDs, de-duplicated
// and in insertion order, so MEAL_ADJUST turns can exclude them.
func (s SessionState) AppendLastRecommendations(ids []int64) SessionState {
	if len(ids) == 0 {
		return s
	}
	seen := make(map[int64]struct{}, len(s.LastRecommendations)+len(ids))
	merged := make([]int64, 0, len(s.LastRecommendations)+len(ids))
	for _, id := range s.LastRecommendations {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	s.LastRecommendations = merged
	return s
}

// MealItem is one meal candidate with its slot tags and match score.
type MealItem struct {
	ID          int64      `json:"id"`
	SourceType  SourceMode `json:"sourceType"`
	OwnerUserID *int64     `json:"ownerUserId"`
	Name        string     `json:"name"`
	Slots       SlotBundle `json:"slots"`
	MatchScore  float64    `json:"matchScore"`
}

// ConversationTurn is one summarized turn of short-term history passed to
// the intent agent.
type ConversationTurn struct {
	Role      string `json:"role"`
	Intent    Intent `json:"intent,omitempty"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}

// IntentResult is the structured output of the intent agent.
type IntentResult struct {
	Intent     Intent     `json:"intent"`
	Slots      SlotBundle `json:"slots"`
	Confidence float64    `json:"confidence"`
}

// ClarifyIntentResult builds the conservative clarify fallback result used
// when the LLM output cannot be parsed.
func ClarifyIntentResult(slots SlotBundle) IntentResult {
	return IntentResult{Intent: IntentClarifyNeeded, Slots: slots, Confidence: 0.2}
}

// ClarifyResult is the clarify node output. The rule layer decides
// ASK/READY; the LLM only phrases the question.
type ClarifyResult struct {
	Action        ClarifyAction `json:"action"`
	QuestionToAsk string        `json:"questionToAsk,omitempty"`
	MissingSlots  []string      `json:"missingSlots"`
}

// ReadyClarifyResult builds a READY result.
func ReadyClarifyResult() ClarifyResult {
	return ClarifyResult{Action: ClarifyReady, MissingSlots: []string{}}
}

// AskClarifyResult builds an ASK result carrying the question and the
// missing slot names.
func AskClarifyResult(question string, missingSlots []string) ClarifyResult {
	if missingSlots == nil {
		missingSlots = []string{}
	}
	return ClarifyResult{Action: ClarifyAsk, QuestionToAsk: question, MissingSlots: missingSlots}
}

// RecommendedMealOption is one recommended meal with its generated reason.
type RecommendedMealOption struct {
	ItemID       int64      `json:"itemId"`
	SourceType   SourceMode `json:"sourceType"`
	Name         string     `json:"name"`
	Reason       string     `json:"reason"`
	MatchScore   float64    `json:"matchScore"`
	MatchedSlots SlotBundle `json:"matchedSlots"`
}

// RecommendResult bundles the recommendation list and the disclaimer flag.
type RecommendResult struct {
	Recommendations []RecommendedMealOption `json:"recommendations"`
	NeedDisclaimer  bool                    `json:"needDisclaimer"`
}

// EmptyRecommendResult builds an empty recommendation result.
func EmptyRecommendResult() RecommendResult {
	return RecommendResult{Recommendations: []RecommendedMealOption{}}
}

// MealResponse is the frontend meal card payload.
type MealResponse struct {
	ID          int64      `json:"id"`
	SourceType  SourceMode `json:"sourceType"`
	Name        string     `json:"name"`
	MealTime    []string   `json:"mealTime"`
	Mood        []string   `json:"mood"`
	Scene       []string   `json:"scene"`
	HealthGoal  []string   `json:"healthGoal"`
	Cuisine     []string   `json:"cuisine"`
	Taste       []string   `json:"taste"`
	Convenience []string   `json:"convenience"`
	MatchScore  float64    `json:"matchScore"`
}

// MealResponseFromItem flattens a MealItem into a card payload.
func MealResponseFromItem(item MealItem) MealResponse {
	return MealResponse{
		ID:          item.ID,
		SourceType:  item.SourceType,
		Name:        item.Name,
		MealTime:    item.Slots.MealTime,
		Mood:        item.Slots.Mood,
		Scene:       item.Slots.Scene,
		HealthGoal:  item.Slots.HealthGoal,
		Cuisine:     item.Slots.Cuisine,
		Taste:       item.Slots.Taste,
		Convenience: item.Slots.Convenience,
		MatchScore:  item.MatchScore,
	}
}

// MealResponseFromOption flattens a recommended option into a card payload.
func MealResponseFromOption(option RecommendedMealOption) MealResponse {
	return MealResponse{
		ID:          option.ItemID,
		SourceType:  option.SourceType,
		Name:        option.Name,
		MealTime:    option.MatchedSlots.MealTime,
		Mood:        option.MatchedSlots.Mood,
		Scene:       option.MatchedSlots.Scene,
		HealthGoal:  option.MatchedSlots.HealthGoal,
		Cuisine:     option.MatchedSlots.Cuisine,
		Taste:       option.MatchedSlots.Taste,
		Convenience: option.MatchedSlots.Convenience,
		MatchScore:  option.MatchScore,
	}
}

// ResponseResult is the user-facing answer: natural speech text plus meal
// cards and the next frontend action.
type ResponseResult struct {
	SpeechText    string         `json:"speechText"`
	DisplayBlocks []MealResponse `json:"displayBlocks"`
	NextAction    string         `json:"nextAction"`
}

// TextOnlyResponse builds a plain-text response without meal cards.
func TextOnlyResponse(text string) ResponseResult {
	return ResponseResult{SpeechText: text, DisplayBlocks: []MealResponse{}, NextAction: "WAIT_USER"}
}

// RiskGuardResult is the health-risk guard outcome.
type RiskGuardResult struct {
	Passed            bool      `json:"passed"`
	RiskLevel         RiskLevel `json:"riskLevel"`
	BlockedReasons    []string  `json:"blockedReasons"`
	RewriteSuggestion string    `json:"rewriteSuggestion,omitempty"`
}

// PassRiskGuardResult builds a low-risk pass result.
func PassRiskGuardResult() RiskGuardResult {
	return RiskGuardResult{Passed: true, RiskLevel: RiskLow, BlockedReasons: []string{}}
}

// BlockRiskGuardResult builds a high-risk block result.
func BlockRiskGuardResult(reasons []string, suggestion string) RiskGuardResult {
	if reasons == nil {
		reasons = []string{}
	}
	return RiskGuardResult{
		Passed:            false,
		RiskLevel:         RiskHigh,
		BlockedReasons:    reasons,
		RewriteSuggestion: suggestion,
	}
}

// ChatRequest is the chat API request body.
type ChatRequest struct {
	SessionID  string         `json:"sessionId"`
	Message    string         `json:"message"`
	SourceMode SourceMode     `json:"sourceMode"`
	Context    map[string]any `json:"context,omitempty"`
}

// ChatResponse is the chat API response body. ResponseType is ANSWER or
// CLARIFY.
type ChatResponse struct {
	SessionID       string         `json:"sessionId"`
	TraceID         string         `json:"traceId,omitempty"`
	ResponseType    string         `json:"responseType"`
	SpeechText      string         `json:"speechText"`
	DisplayBlocks   []MealResponse `json:"displayBlocks"`
	NextAction      string         `json:"nextAction"`
	ClarifyQuestion string         `json:"clarifyQuestion,omitempty"`
	MissingSlots    []string       `json:"missingSlots"`
}

// AnswerChatResponse builds an ANSWER response with meal cards.
func AnswerChatResponse(sessionID, traceID, speechText string, displayBlocks []MealResponse, nextAction string) ChatResponse {
	if displayBlocks == nil {
		displayBlocks = []MealResponse{}
	}
	return ChatResponse{
		SessionID:     sessionID,
		TraceID:       traceID,
		ResponseType:  "ANSWER",
		SpeechText:    speechText,
		DisplayBlocks: displayBlocks,
		NextAction:    nextAction,
		MissingSlots:  []string{},
	}
}

// ClarifyChatResponse builds a CLARIFY response carrying the question and
// the missing slot names.
func ClarifyChatResponse(sessionID, traceID, question string, missingSlots []string) ChatResponse {
	if missingSlots == nil {
		missingSlots = []string{}
	}
	return ChatResponse{
		SessionID:       sessionID,
		TraceID:         traceID,
		ResponseType:    "CLARIFY",
		SpeechText:      question,
		DisplayBlocks:   []MealResponse{},
		NextAction:      "ASK_CLARIFY",
		ClarifyQuestion: question,
		MissingSlots:    missingSlots,
	}
}

// MealRequest is the create/update body for a personal meal.
type MealRequest struct {
	Name        string   `json:"name"`
	MealTime    []string `json:"mealTime"`
	Mood        []string `json:"mood"`
	Scene       []string `json:"scene"`
	HealthGoal  []string `json:"healthGoal"`
	Cuisine     []string `json:"cuisine"`
	Taste       []string `json:"taste"`
	Convenience []string `json:"convenience"`
}

// ToSlots converts the request into a normalized SlotBundle.
func (r MealRequest) ToSlots() SlotBundle {
	return NewSlotBundle(r.MealTime, r.Mood, r.Scene, r.HealthGoal, r.Cuisine, r.Taste, r.Convenience)
}

// FeedbackRequest is the recommendation feedback body.
type FeedbackRequest struct {
	SessionID string  `json:"sessionId"`
	ItemID    *int64  `json:"itemId"`
	Action    string  `json:"action"`
	Rating    *int    `json:"rating"`
	Reason    *string `json:"reason"`
}

// TraceLabelRequest stores the human gold label of one trace.
type TraceLabelRequest struct {
	ExpectedIntent        Intent        `json:"expectedIntent"`
	ExpectedSlots         *SlotBundle   `json:"expectedSlots"`
	ExpectedClarifyAction ClarifyAction `json:"expectedClarifyAction"`
	LabelNote             *string       `json:"labelNote"`
}

// EvaluationRequest selects the traces to evaluate.
type EvaluationRequest struct {
	StartAt         FlexTime `json:"startAt"`
	EndAt           FlexTime `json:"endAt"`
	IncludeLLMJudge *bool    `json:"includeLlmJudge"`
	Limit           *int     `json:"limit"`
}

// EvaluationJudgeResult is the LLM-as-judge output for one trace.
type EvaluationJudgeResult struct {
	ExplanationQuality float64 `json:"explanationQuality"`
	Naturalness        float64 `json:"naturalness"`
	Reason             string  `json:"reason"`
}

// TraceEvaluationResult is the per-trace evaluation detail.
type TraceEvaluationResult struct {
	TraceID           string              `json:"traceId"`
	SessionID         string              `json:"sessionId"`
	CreatedAt         *time.Time          `json:"createdAt"`
	Score             *float64            `json:"score"`
	RuleScore         *float64            `json:"ruleScore"`
	LLMJudgeScore     *float64            `json:"llmJudgeScore"`
	UserFeedbackScore *float64            `json:"userFeedbackScore"`
	Metrics           map[string]*float64 `json:"metrics"`
	Detail            map[string]any      `json:"detail"`
}

// EvaluationReport aggregates evaluation results over a time window.
type EvaluationReport struct {
	StartAt        time.Time               `json:"startAt"`
	EndAt          time.Time               `json:"endAt"`
	TotalTraces    int                     `json:"totalTraces"`
	LabeledTraces  int                     `json:"labeledTraces"`
	AvgScore       *float64                `json:"avgScore"`
	MetricAverages map[string]*float64     `json:"metricAverages"`
	TraceResults   []TraceEvaluationResult `json:"traceResults"`
}

// FlexTime parses the ISO date-time formats accepted by the API:
// RFC 3339 and the second/minute-precision local formats emitted by HTML
// datetime-local inputs.
type FlexTime time.Time

// Time returns the wrapped time value.
func (f FlexTime) Time() time.Time { return time.Time(f) }

// IsZero reports whether no time was provided.
func (f FlexTime) IsZero() bool { return time.Time(f).IsZero() }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexTime) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseFlexTime(raw)
	if err != nil {
		return err
	}
	*f = FlexTime(parsed)
	return nil
}

// MarshalJSON implements json.Marshaler, emitting an ISO local date-time.
func (f FlexTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(FormatTime(f.Time()))
}

// flexLayouts are tried in order when parsing API timestamps.
var flexLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
}

// ParseFlexTime parses an API timestamp string in local time.
func ParseFlexTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	for _, layout := range flexLayouts {
		if parsed, err := time.ParseInLocation(layout, trimmed, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, &TimeParseError{Value: raw}
}

// TimeParseError reports an unparsable timestamp.
type TimeParseError struct{ Value string }

// Error implements the error interface.
func (e *TimeParseError) Error() string {
	return "invalid datetime value: " + e.Value
}

// FormatTime renders a time as an ISO local date-time string without zone
// suffix, matching the Java API output.
func FormatTime(value time.Time) string {
	return value.Format("2006-01-02T15:04:05")
}
