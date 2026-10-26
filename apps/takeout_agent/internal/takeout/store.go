package takeout

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/hangtiancheng/yukino.go/apps/takeout_agent/internal/config"
)

// Store is the MySQL persistence layer, replacing the MyBatis mappers of
// the Java implementation. It is backed by GORM.
type Store struct {
	db *gorm.DB
}

// NewStore opens the MySQL connection pool described by cfg.
func NewStore(cfg config.DatabaseConfig) (*Store, error) {
	gormDB, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		// The application logs through slog and surfaces every store error
		// to the caller, so GORM's own logger stays silent to avoid
		// duplicate, differently formatted output.
		Logger: logger.Default.LogMode(logger.Silent),
		// Each store method issues a single write, so skip GORM's default
		// per-write transaction and keep plain autocommit semantics.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, WrapError(err, "open mysql failed")
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, WrapError(err, "open mysql pool failed")
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)
	if err := sqlDB.Ping(); err != nil {
		return nil, WrapError(err, "connect mysql failed")
	}
	return &Store{db: gormDB}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return WrapError(err, "close mysql pool failed")
	}
	return sqlDB.Close()
}

// SessionRow maps a takeout_sessions row.
type SessionRow struct {
	ID                  string    `json:"id" gorm:"column:id;primaryKey"`
	UserID              int64     `json:"userId" gorm:"column:user_id"`
	Phase               string    `json:"phase" gorm:"column:phase"`
	Slots               string    `json:"slots" gorm:"column:slots"`
	LastRecommendations string    `json:"lastRecommendations" gorm:"column:last_recommendations"`
	CreatedAt           time.Time `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt           time.Time `json:"updatedAt" gorm:"column:updated_at"`
}

// TableName binds SessionRow to takeout_sessions.
func (SessionRow) TableName() string { return "takeout_sessions" }

// SessionMessageRow maps a takeout_messages row.
type SessionMessageRow struct {
	ID           int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	SessionID    string    `json:"sessionId" gorm:"column:session_id"`
	Role         string    `json:"role" gorm:"column:role"`
	Content      string    `json:"content" gorm:"column:content"`
	Intent       *string   `json:"intent" gorm:"column:intent"`
	AgentTraceID *string   `json:"agentTraceId" gorm:"column:agent_trace_id"`
	CreatedAt    time.Time `json:"createdAt" gorm:"column:created_at"`
}

// TableName binds SessionMessageRow to takeout_messages.
func (SessionMessageRow) TableName() string { return "takeout_messages" }

// MealItemRow maps a meal_item row; slot columns hold JSON arrays.
type MealItemRow struct {
	ID          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	SourceType  string    `json:"sourceType" gorm:"column:source_type"`
	OwnerUserID *int64    `json:"ownerUserId" gorm:"column:owner_user_id"`
	Name        string    `json:"name" gorm:"column:name"`
	MealTime    string    `json:"mealTime" gorm:"column:meal_time"`
	Mood        string    `json:"mood" gorm:"column:mood"`
	Scene       string    `json:"scene" gorm:"column:scene"`
	HealthGoal  string    `json:"healthGoal" gorm:"column:health_goal"`
	Cuisine     string    `json:"cuisine" gorm:"column:cuisine"`
	Taste       string    `json:"taste" gorm:"column:taste"`
	Convenience string    `json:"convenience" gorm:"column:convenience"`
	CreatedAt   time.Time `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt   time.Time `json:"updatedAt" gorm:"column:updated_at"`
}

// TableName binds MealItemRow to meal_item.
func (MealItemRow) TableName() string { return "meal_item" }

// RequestTraceRow maps a takeout_request_trace row.
type RequestTraceRow struct {
	ID                    int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	TraceID               string     `json:"traceId" gorm:"column:trace_id"`
	SessionID             string     `json:"sessionId" gorm:"column:session_id"`
	UserID                int64      `json:"userId" gorm:"column:user_id"`
	Status                string     `json:"status" gorm:"column:status"`
	EventCount            int        `json:"eventCount" gorm:"column:event_count"`
	DurationMs            *int64     `json:"durationMs" gorm:"column:duration_ms"`
	ErrorMessage          *string    `json:"errorMessage" gorm:"column:error_message"`
	TraceJSON             string     `json:"traceJson" gorm:"column:trace_json"`
	ExpectedIntent        *string    `json:"expectedIntent" gorm:"column:expected_intent"`
	ExpectedSlots         *string    `json:"expectedSlots" gorm:"column:expected_slots"`
	ExpectedClarifyAction *string    `json:"expectedClarifyAction" gorm:"column:expected_clarify_action"`
	LabeledBy             *int64     `json:"labeledBy" gorm:"column:labeled_by"`
	LabeledAt             *time.Time `json:"labeledAt" gorm:"column:labeled_at"`
	LabelNote             *string    `json:"labelNote" gorm:"column:label_note"`
	CreatedAt             time.Time  `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt             time.Time  `json:"updatedAt" gorm:"column:updated_at"`
}

// TableName binds RequestTraceRow to takeout_request_trace.
func (RequestTraceRow) TableName() string { return "takeout_request_trace" }

// FeedbackRow maps a recommend_feedback row.
type FeedbackRow struct {
	ID        int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64     `json:"userId" gorm:"column:user_id"`
	SessionID string    `json:"sessionId" gorm:"column:session_id"`
	ItemID    *int64    `json:"itemId" gorm:"column:item_id"`
	Action    string    `json:"action" gorm:"column:action"`
	Rating    *int      `json:"rating" gorm:"column:rating"`
	Reason    *string   `json:"reason" gorm:"column:reason"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at"`
}

// TableName binds FeedbackRow to recommend_feedback.
func (FeedbackRow) TableName() string { return "recommend_feedback" }

// SlotOptionRow maps a takeout_slot_option row.
type SlotOptionRow struct {
	ID          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	SlotName    string    `json:"slotName" gorm:"column:slot_name"`
	OptionValue string    `json:"optionValue" gorm:"column:option_value"`
	SortOrder   int       `json:"sortOrder" gorm:"column:sort_order"`
	Enabled     int8      `json:"enabled" gorm:"column:enabled"`
	CreatedAt   time.Time `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt   time.Time `json:"updatedAt" gorm:"column:updated_at"`
}

// TableName binds SlotOptionRow to takeout_slot_option.
func (SlotOptionRow) TableName() string { return "takeout_slot_option" }

// ---------------------------------------------------------------------------
// takeout_sessions / takeout_messages
// ---------------------------------------------------------------------------

// InsertSession inserts one session row.
func (s *Store) InsertSession(row SessionRow) error {
	err := s.db.Model(&SessionRow{}).Create(map[string]any{
		"id":                   row.ID,
		"user_id":              row.UserID,
		"phase":                row.Phase,
		"slots":                row.Slots,
		"last_recommendations": row.LastRecommendations,
		"created_at":           gorm.Expr("NOW()"),
		"updated_at":           gorm.Expr("NOW()"),
	}).Error
	if err != nil {
		return WrapError(err, "insert session failed")
	}
	return nil
}

// FindSessionByID loads one session owned by the user.
func (s *Store) FindSessionByID(sessionID string, userID int64) (*SessionRow, error) {
	var row SessionRow
	err := s.db.Where("id = ? AND user_id = ?", sessionID, userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, WrapError(err, "query session failed")
	}
	return &row, nil
}

// UpdateSession persists phase/slots/last_recommendations changes and
// returns the affected row count.
func (s *Store) UpdateSession(row SessionRow) (int64, error) {
	result := s.db.Model(&SessionRow{}).
		Where("id = ? AND user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"phase":                row.Phase,
			"slots":                row.Slots,
			"last_recommendations": row.LastRecommendations,
			"updated_at":           gorm.Expr("NOW()"),
		})
	if result.Error != nil {
		return 0, WrapError(result.Error, "update session failed")
	}
	return result.RowsAffected, nil
}

// InsertMessage appends one conversation message.
func (s *Store) InsertMessage(sessionID, role, content string, intent, traceID *string) error {
	err := s.db.Model(&SessionMessageRow{}).Create(map[string]any{
		"session_id":     sessionID,
		"role":           role,
		"content":        content,
		"intent":         intent,
		"agent_trace_id": traceID,
		"created_at":     gorm.Expr("NOW()"),
	}).Error
	if err != nil {
		return WrapError(err, "insert message failed")
	}
	return nil
}

// ListRecentMessages returns the n most recent messages of a session in
// descending order. The join against takeout_sessions enforces ownership.
func (s *Store) ListRecentMessages(sessionID string, userID int64, limit int) ([]SessionMessageRow, error) {
	var rows []SessionMessageRow
	err := s.db.Model(&SessionMessageRow{}).
		Select("takeout_messages.id, takeout_messages.session_id, takeout_messages.role, takeout_messages.content, takeout_messages.intent, takeout_messages.agent_trace_id, takeout_messages.created_at").
		Joins("INNER JOIN takeout_sessions ON takeout_sessions.id = takeout_messages.session_id AND takeout_sessions.user_id = ?", userID).
		Where("takeout_messages.session_id = ?", sessionID).
		Order("takeout_messages.created_at DESC, takeout_messages.id DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, WrapError(err, "query messages failed")
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// meal_item
// ---------------------------------------------------------------------------

// InsertMeal inserts one meal and writes the generated ID back to the row.
func (s *Store) InsertMeal(row *MealItemRow) error {
	data := map[string]any{
		"source_type":   row.SourceType,
		"owner_user_id": row.OwnerUserID,
		"name":          row.Name,
		"meal_time":     row.MealTime,
		"mood":          row.Mood,
		"scene":         row.Scene,
		"health_goal":   row.HealthGoal,
		"cuisine":       row.Cuisine,
		"taste":         row.Taste,
		"convenience":   row.Convenience,
		"created_at":    gorm.Expr("NOW()"),
		"updated_at":    gorm.Expr("NOW()"),
	}
	if err := s.db.Model(&MealItemRow{}).Create(data).Error; err != nil {
		return WrapError(err, "insert meal failed")
	}
	if id, ok := data["id"].(int64); ok {
		row.ID = id
	}
	return nil
}

// UpdatePersonalMeal updates one PERSONAL meal owned by the user.
func (s *Store) UpdatePersonalMeal(row MealItemRow) (int64, error) {
	result := s.db.Model(&MealItemRow{}).
		Where("id = ? AND source_type = 'PERSONAL' AND owner_user_id = ?", row.ID, row.OwnerUserID).
		Updates(map[string]any{
			"name":        row.Name,
			"meal_time":   row.MealTime,
			"mood":        row.Mood,
			"scene":       row.Scene,
			"health_goal": row.HealthGoal,
			"cuisine":     row.Cuisine,
			"taste":       row.Taste,
			"convenience": row.Convenience,
			"updated_at":  gorm.Expr("NOW()"),
		})
	if result.Error != nil {
		return 0, WrapError(result.Error, "update meal failed")
	}
	return result.RowsAffected, nil
}

// DeletePersonalMeal deletes one PERSONAL meal owned by the user.
func (s *Store) DeletePersonalMeal(mealID, userID int64) (int64, error) {
	result := s.db.
		Where("id = ? AND source_type = 'PERSONAL' AND owner_user_id = ?", mealID, userID).
		Delete(&MealItemRow{})
	if result.Error != nil {
		return 0, WrapError(result.Error, "delete meal failed")
	}
	return result.RowsAffected, nil
}

// FindPersonalMealByID loads one PERSONAL meal owned by the user.
func (s *Store) FindPersonalMealByID(mealID, userID int64) (*MealItemRow, error) {
	var row MealItemRow
	err := s.db.
		Where("id = ? AND source_type = 'PERSONAL' AND owner_user_id = ?", mealID, userID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, WrapError(err, "query meal failed")
	}
	return &row, nil
}

// FindPersonalMeals lists the PERSONAL meals of one user.
func (s *Store) FindPersonalMeals(userID int64) ([]MealItemRow, error) {
	var rows []MealItemRow
	err := s.db.
		Where("source_type = 'PERSONAL' AND owner_user_id = ?", userID).
		Order("updated_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, WrapError(err, "query personal meals failed")
	}
	return rows, nil
}

// FindPublicMeals lists the PUBLIC meal library.
func (s *Store) FindPublicMeals() ([]MealItemRow, error) {
	var rows []MealItemRow
	err := s.db.
		Where("source_type = 'PUBLIC' AND owner_user_id IS NULL").
		Order("updated_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, WrapError(err, "query public meals failed")
	}
	return rows, nil
}

// CountPersonalMeals counts the PERSONAL meals of one user.
func (s *Store) CountPersonalMeals(userID int64) (int, error) {
	var count int64
	err := s.db.Model(&MealItemRow{}).
		Where("source_type = 'PERSONAL' AND owner_user_id = ?", userID).
		Count(&count).Error
	if err != nil {
		return 0, WrapError(err, "count personal meals failed")
	}
	return int(count), nil
}

// SearchMeals recalls candidates with MySQL JSON_OVERLAPS: a dimension
// whose query JSON is "[]" matches everything, otherwise the stored tags
// must overlap the query tags.
func (s *Store) SearchMeals(sourceMode SourceMode, userID int64, slots SlotBundle, limit int) ([]MealItemRow, error) {
	query := s.db.Model(&MealItemRow{})
	if sourceMode == SourcePersonal {
		query = query.Where("source_type = 'PERSONAL' AND owner_user_id = ?", userID)
	} else {
		query = query.Where("source_type = 'PUBLIC' AND owner_user_id IS NULL")
	}
	for _, dimension := range []struct {
		column string
		values []string
	}{
		{"meal_time", slots.MealTime},
		{"mood", slots.Mood},
		{"scene", slots.Scene},
		{"health_goal", slots.HealthGoal},
		{"cuisine", slots.Cuisine},
		{"taste", slots.Taste},
		{"convenience", slots.Convenience},
	} {
		queryJSON, err := json.Marshal(normalizeLabels(dimension.values))
		if err != nil {
			return nil, WrapError(err, "marshal slot query failed")
		}
		query = query.Where(
			fmt.Sprintf("(? = '[]' OR JSON_OVERLAPS(%s, ?))", dimension.column),
			string(queryJSON), string(queryJSON),
		)
	}
	var rows []MealItemRow
	if err := query.Order("updated_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, WrapError(err, "search meals failed")
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// takeout_slot_option
// ---------------------------------------------------------------------------

// FindEnabledSlotValues lists the enabled option values of one slot,
// ordered by sort_order.
func (s *Store) FindEnabledSlotValues(slotName string) ([]string, error) {
	var values []string
	err := s.db.Model(&SlotOptionRow{}).
		Where("slot_name = ? AND enabled = 1", slotName).
		Order("sort_order ASC, id ASC").
		Pluck("option_value", &values).Error
	if err != nil {
		return nil, WrapError(err, "query slot options failed")
	}
	return values, nil
}

// ---------------------------------------------------------------------------
// takeout_request_trace
// ---------------------------------------------------------------------------

// InsertTrace persists one flushed request trace.
func (s *Store) InsertTrace(row RequestTraceRow) error {
	err := s.db.Model(&RequestTraceRow{}).Create(map[string]any{
		"trace_id":      row.TraceID,
		"session_id":    row.SessionID,
		"user_id":       row.UserID,
		"status":        row.Status,
		"event_count":   row.EventCount,
		"duration_ms":   row.DurationMs,
		"error_message": row.ErrorMessage,
		"trace_json":    row.TraceJSON,
		"created_at":    gorm.Expr("NOW()"),
		"updated_at":    gorm.Expr("NOW()"),
	}).Error
	if err != nil {
		return WrapError(err, "insert trace failed")
	}
	return nil
}

// FindTraceByID loads one trace by trace ID scoped to the user.
func (s *Store) FindTraceByID(userID int64, traceID string) (*RequestTraceRow, error) {
	var row RequestTraceRow
	err := s.db.
		Where("user_id = ? AND trace_id = ?", userID, traceID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, WrapError(err, "query trace failed")
	}
	return &row, nil
}

// FindTracesBySession lists the most recent traces of one session.
func (s *Store) FindTracesBySession(userID int64, sessionID string, limit int) ([]RequestTraceRow, error) {
	var rows []RequestTraceRow
	err := s.db.
		Where("user_id = ? AND session_id = ?", userID, sessionID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, WrapError(err, "query session traces failed")
	}
	return rows, nil
}

// FindTracesByTimeRange lists traces inside [startAt, endAt), optionally
// restricted to unlabeled rows.
func (s *Store) FindTracesByTimeRange(userID int64, startAt, endAt time.Time, onlyUnlabeled bool, limit int) ([]RequestTraceRow, error) {
	query := s.db.Model(&RequestTraceRow{}).
		Where("user_id = ? AND created_at >= ? AND created_at < ?", userID, startAt, endAt)
	if onlyUnlabeled {
		query = query.Where("expected_intent IS NULL AND expected_slots IS NULL AND expected_clarify_action IS NULL")
	}
	var rows []RequestTraceRow
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, WrapError(err, "query traces by time range failed")
	}
	return rows, nil
}

// UpdateTraceLabel writes the gold label fields of one trace and returns
// the affected row count.
func (s *Store) UpdateTraceLabel(
	userID int64,
	traceID string,
	expectedIntent, expectedSlots, expectedClarifyAction *string,
	labeledBy *int64,
	labelNote *string,
) (int64, error) {
	result := s.db.Model(&RequestTraceRow{}).
		Where("user_id = ? AND trace_id = ?", userID, traceID).
		Updates(map[string]any{
			"expected_intent":         expectedIntent,
			"expected_slots":          expectedSlots,
			"expected_clarify_action": expectedClarifyAction,
			"labeled_by":              labeledBy,
			"labeled_at":              gorm.Expr("NOW()"),
			"label_note":              labelNote,
			"updated_at":              gorm.Expr("NOW()"),
		})
	if result.Error != nil {
		return 0, WrapError(result.Error, "update trace label failed")
	}
	return result.RowsAffected, nil
}

// ---------------------------------------------------------------------------
// recommend_feedback
// ---------------------------------------------------------------------------

// InsertFeedback persists one feedback record.
func (s *Store) InsertFeedback(userID int64, request FeedbackRequest) error {
	err := s.db.Model(&FeedbackRow{}).Create(map[string]any{
		"user_id":    userID,
		"session_id": request.SessionID,
		"item_id":    request.ItemID,
		"action":     request.Action,
		"rating":     request.Rating,
		"reason":     request.Reason,
		"created_at": gorm.Expr("NOW()"),
	}).Error
	if err != nil {
		return WrapError(err, "insert feedback failed")
	}
	return nil
}

// FindFeedbackBySessions lists feedback of the given sessions inside
// [startAt, endAt). An empty session list returns no rows.
func (s *Store) FindFeedbackBySessions(userID int64, sessionIDs []string, startAt, endAt time.Time) ([]FeedbackRow, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []FeedbackRow
	err := s.db.Model(&FeedbackRow{}).
		Where("user_id = ? AND created_at >= ? AND created_at < ? AND session_id IN ?", userID, startAt, endAt, sessionIDs).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, WrapError(err, "query feedback failed")
	}
	return rows, nil
}
