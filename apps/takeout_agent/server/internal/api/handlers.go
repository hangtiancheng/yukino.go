package api

import (
	"strconv"
	"strings"
	"time"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"

	"github.com/hangtiancheng/yukino.go/apps/takeout_agent/server/internal/takeout"
)

// CreateSessionResponse is the POST /sessions response body.
type CreateSessionResponse struct {
	SessionID string `json:"sessionId"`
}

// handleChat runs one orchestrator turn.
func (s *Services) handleChat(ctx *yukino.Context, userID int64) (any, error) {
	var request takeout.ChatRequest
	if err := ctx.BindJSON(&request); err != nil {
		return nil, takeout.NewError("invalid JSON body")
	}
	return s.Orchestrator.TakeoutChat(ctx.Request.Context(), userID, request)
}

// handleCreateSession creates an empty session row.
func (s *Services) handleCreateSession(ctx *yukino.Context, userID int64) (any, error) {
	sessionID, err := s.Sessions.CreateSession(userID)
	if err != nil {
		return nil, err
	}
	return CreateSessionResponse{SessionID: sessionID}, nil
}

// handleListPersonalMeals returns the personal meal library of the user.
func (s *Services) handleListPersonalMeals(ctx *yukino.Context, userID int64) (any, error) {
	meals, err := s.Meals.FindPersonalMeals(userID)
	if err != nil {
		return nil, err
	}
	return toMealResponses(meals), nil
}

// handleCreatePersonalMeal inserts one personal meal.
func (s *Services) handleCreatePersonalMeal(ctx *yukino.Context, userID int64) (any, error) {
	var request takeout.MealRequest
	if err := ctx.BindJSON(&request); err != nil {
		return nil, takeout.NewError("invalid JSON body")
	}
	meal, err := s.Meals.CreatePersonalMeal(userID, request)
	if err != nil {
		return nil, err
	}
	return takeout.MealResponseFromItem(meal), nil
}

// handleUpdatePersonalMeal updates one personal meal.
func (s *Services) handleUpdatePersonalMeal(ctx *yukino.Context, userID int64) (any, error) {
	mealID, err := parseMealID(ctx)
	if err != nil {
		return nil, err
	}
	var request takeout.MealRequest
	if err := ctx.BindJSON(&request); err != nil {
		return nil, takeout.NewError("invalid JSON body")
	}
	meal, err := s.Meals.UpdatePersonalMeal(userID, mealID, request)
	if err != nil {
		return nil, err
	}
	return takeout.MealResponseFromItem(meal), nil
}

// handleDeletePersonalMeal deletes one personal meal; success renders 204.
func (s *Services) handleDeletePersonalMeal(ctx *yukino.Context, userID int64) (any, error) {
	mealID, err := parseMealID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Meals.DeletePersonalMeal(userID, mealID); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleListPublicMeals returns the public meal library.
func (s *Services) handleListPublicMeals(ctx *yukino.Context, userID int64) (any, error) {
	meals, err := s.Meals.FindPublicMeals()
	if err != nil {
		return nil, err
	}
	return toMealResponses(meals), nil
}

// handleSlotOptions returns the enabled slot dictionary.
func (s *Services) handleSlotOptions(ctx *yukino.Context, userID int64) (any, error) {
	return s.SlotOptions.FindAllOptions()
}

// handleEvaluate runs the evaluation pipeline over a time window.
func (s *Services) handleEvaluate(ctx *yukino.Context, userID int64) (any, error) {
	var request takeout.EvaluationRequest
	if err := ctx.BindJSON(&request); err != nil {
		return nil, takeout.NewError("invalid JSON body")
	}
	return s.Evaluations.Evaluate(ctx.Request.Context(), userID, request)
}

// handleSaveFeedback persists recommendation feedback; success renders 204.
func (s *Services) handleSaveFeedback(ctx *yukino.Context, userID int64) (any, error) {
	var request takeout.FeedbackRequest
	if err := ctx.BindJSON(&request); err != nil {
		return nil, takeout.NewError("invalid JSON body")
	}
	if err := s.Feedback.Save(userID, request); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleListTraces queries traces by time range.
func (s *Services) handleListTraces(ctx *yukino.Context, userID int64) (any, error) {
	startAt, err := parseQueryTime(ctx, "startAt")
	if err != nil {
		return nil, err
	}
	endAt, err := parseQueryTime(ctx, "endAt")
	if err != nil {
		return nil, err
	}
	onlyUnlabeled := strings.EqualFold(ctx.Query("onlyUnlabeled"), "true")
	limit, err := parseQueryLimit(ctx.Query("limit"))
	if err != nil {
		return nil, err
	}
	traces, err := s.Traces.FindByTimeRange(userID, startAt, endAt, onlyUnlabeled, limit)
	if err != nil {
		return nil, err
	}
	return tracesOrEmpty(traces), nil
}

// handleGetTrace returns one trace by ID.
func (s *Services) handleGetTrace(ctx *yukino.Context, userID int64) (any, error) {
	traceID := ctx.Param("traceId")
	row, err := s.Traces.FindByTraceID(userID, traceID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, takeout.NewNotFoundError("trace not found: %s", traceID)
	}
	return row, nil
}

// handleListSessionTraces returns the recent traces of one session.
func (s *Services) handleListSessionTraces(ctx *yukino.Context, userID int64) (any, error) {
	sessionID := ctx.Param("sessionId")
	limit, err := parseQueryLimit(ctx.Query("limit"))
	if err != nil {
		return nil, err
	}
	traces, err := s.Traces.FindBySessionID(userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	return tracesOrEmpty(traces), nil
}

// handleLabelTrace stores the gold label of one trace; success renders 204.
func (s *Services) handleLabelTrace(ctx *yukino.Context, userID int64) (any, error) {
	traceID := ctx.Param("traceId")
	var request takeout.TraceLabelRequest
	if err := ctx.BindJSON(&request); err != nil {
		return nil, takeout.NewError("invalid JSON body")
	}
	if err := s.Traces.UpdateLabel(userID, traceID, request); err != nil {
		return nil, err
	}
	return nil, nil
}

func toMealResponses(meals []takeout.MealItem) []takeout.MealResponse {
	responses := make([]takeout.MealResponse, 0, len(meals))
	for _, meal := range meals {
		responses = append(responses, takeout.MealResponseFromItem(meal))
	}
	return responses
}

func tracesOrEmpty(traces []takeout.RequestTraceRow) []takeout.RequestTraceRow {
	if traces == nil {
		return []takeout.RequestTraceRow{}
	}
	return traces
}

func parseMealID(ctx *yukino.Context) (int64, error) {
	mealID, err := strconv.ParseInt(ctx.Param("mealId"), 10, 64)
	if err != nil || mealID <= 0 {
		return 0, takeout.NewError("invalid mealId")
	}
	return mealID, nil
}

func parseQueryTime(ctx *yukino.Context, key string) (time.Time, error) {
	raw := ctx.Query(key)
	if raw == "" {
		return time.Time{}, takeout.NewError("%s is required", key)
	}
	parsed, err := takeout.ParseFlexTime(raw)
	if err != nil {
		return time.Time{}, takeout.NewError("invalid %s: %s", key, raw)
	}
	return parsed, nil
}

func parseQueryLimit(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return nil, takeout.NewError("invalid limit: %s", raw)
	}
	return &limit, nil
}
