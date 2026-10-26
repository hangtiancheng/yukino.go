package takeout

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// SessionService persists conversation messages and creates session rows.
// Session state (slots/phase) is managed by SessionStateService.
type SessionService struct {
	store           *Store
	maxHistoryTurns int
}

// NewSessionService builds the session message service.
func NewSessionService(store *Store, maxHistoryTurns int) *SessionService {
	if maxHistoryTurns < 1 {
		maxHistoryTurns = 10
	}
	return &SessionService{store: store, maxHistoryTurns: maxHistoryTurns}
}

// NewSessionID generates a session ID in the sess_<32 hex> format.
func NewSessionID() string {
	return "sess_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// CreateSession inserts a fresh session row and returns its ID.
func (s *SessionService) CreateSession(userID int64) (string, error) {
	row := SessionRow{
		ID:                  NewSessionID(),
		UserID:              userID,
		Phase:               string(PhaseStart),
		Slots:               "{}",
		LastRecommendations: "[]",
	}
	if err := s.store.InsertSession(row); err != nil {
		return "", err
	}
	return row.ID, nil
}

// EnsureSession inserts an empty session row when the ID does not exist yet.
func (s *SessionService) EnsureSession(sessionID string, userID int64) error {
	existing, err := s.store.FindSessionByID(sessionID, userID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	return s.store.InsertSession(SessionRow{
		ID:                  sessionID,
		UserID:              userID,
		Phase:               string(PhaseStart),
		Slots:               "{}",
		LastRecommendations: "[]",
	})
}

// AppendMessage inserts one conversation message.
func (s *SessionService) AppendMessage(sessionID, role, content string, intent *Intent, traceID string) error {
	var intentValue *string
	if intent != nil {
		name := string(*intent)
		intentValue = &name
	}
	var traceValue *string
	if traceID != "" {
		traceValue = &traceID
	}
	return s.store.InsertMessage(sessionID, role, content, intentValue, traceValue)
}

// RecentConversationTurns loads the n most recent messages of a session as
// chronological short-term context for the intent agent.
func (s *SessionService) RecentConversationTurns(sessionID string, userID int64, n int) ([]ConversationTurn, error) {
	if strings.TrimSpace(sessionID) == "" || userID == 0 || n <= 0 {
		return []ConversationTurn{}, nil
	}
	limit := n
	if limit > s.maxHistoryTurns {
		limit = s.maxHistoryTurns
	}
	if limit < 1 {
		limit = 1
	}
	rows, err := s.store.ListRecentMessages(sessionID, userID, limit)
	if err != nil {
		return nil, err
	}
	// Rows come back newest-first; the prompt expects chronological order.
	turns := make([]ConversationTurn, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		turn := ConversationTurn{
			Role:      row.Role,
			Summary:   summarizeText(row.Content),
			Timestamp: row.CreatedAt.UnixMilli(),
		}
		if row.Intent != nil {
			if intent, ok := ParseIntent(*row.Intent); ok {
				turn.Intent = intent
			}
		}
		turns = append(turns, turn)
	}
	return turns, nil
}

// summarizeText compresses long messages to at most 120 characters to
// save intent-agent tokens.
func summarizeText(text string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\n", " "))
	runes := []rune(normalized)
	if len(runes) <= 120 {
		return normalized
	}
	return string(runes[:120])
}

// SessionStateService reads and writes the SessionState of a session. The
// orchestrator is the only writer during a chat turn.
type SessionStateService struct {
	store *Store
}

// NewSessionStateService builds the session state service.
func NewSessionStateService(store *Store) *SessionStateService {
	return &SessionStateService{store: store}
}

// Create generates a fresh session ID, inserts its row, and returns the
// initial state.
func (s *SessionStateService) Create(userID int64, sourceMode SourceMode) (SessionState, error) {
	state := FreshSessionState(NewSessionID(), userID, sourceMode)
	if err := s.insert(state); err != nil {
		return SessionState{}, err
	}
	return state, nil
}

// LoadOrCreate loads the state of an existing session or creates one when
// the ID is missing or unknown.
func (s *SessionStateService) LoadOrCreate(sessionID string, userID int64, sourceMode SourceMode) (SessionState, error) {
	if strings.TrimSpace(sessionID) == "" {
		return s.Create(userID, sourceMode)
	}
	row, err := s.store.FindSessionByID(sessionID, userID)
	if err != nil {
		return SessionState{}, err
	}
	if row == nil {
		state := FreshSessionState(sessionID, userID, sourceMode)
		if err := s.insert(state); err != nil {
			return SessionState{}, err
		}
		return state, nil
	}
	return stateFromRow(*row, sourceMode)
}

// Save persists orchestrator state updates; zero affected rows means the
// session vanished or a concurrent writer won.
func (s *SessionStateService) Save(state SessionState) error {
	row, err := stateToRow(state)
	if err != nil {
		return err
	}
	updated, err := s.store.UpdateSession(row)
	if err != nil {
		return err
	}
	if updated == 0 {
		return NewError("failed to save session state")
	}
	return nil
}

func (s *SessionStateService) insert(state SessionState) error {
	row, err := stateToRow(state)
	if err != nil {
		return err
	}
	return s.store.InsertSession(row)
}

// slotsDocument is the JSON shape stored in takeout_sessions.slots: the seven
// slot dimensions plus a _meta object with sourceMode and currentIntent.
type slotsDocument struct {
	MealTime    []string          `json:"mealTime"`
	Mood        []string          `json:"mood"`
	Scene       []string          `json:"scene"`
	HealthGoal  []string          `json:"healthGoal"`
	Cuisine     []string          `json:"cuisine"`
	Taste       []string          `json:"taste"`
	Convenience []string          `json:"convenience"`
	Meta        slotsMetaDocument `json:"_meta"`
}

type slotsMetaDocument struct {
	SourceMode    *string `json:"sourceMode"`
	CurrentIntent *string `json:"currentIntent"`
}

// stateFromRow deserializes a database row into a SessionState.
func stateFromRow(row SessionRow, requestSourceMode SourceMode) (SessionState, error) {
	var document slotsDocument
	if strings.TrimSpace(row.Slots) != "" {
		if err := json.Unmarshal([]byte(row.Slots), &document); err != nil {
			return SessionState{}, WrapError(err, "failed to parse session state")
		}
	}
	sourceMode := requestSourceMode
	if document.Meta.SourceMode != nil {
		if parsed, ok := ParseSourceMode(*document.Meta.SourceMode); ok {
			sourceMode = parsed
		}
	}
	var currentIntent Intent
	if document.Meta.CurrentIntent != nil {
		if parsed, ok := ParseIntent(*document.Meta.CurrentIntent); ok {
			currentIntent = parsed
		}
	}
	var lastRecommendations []int64
	if strings.TrimSpace(row.LastRecommendations) != "" {
		if err := json.Unmarshal([]byte(row.LastRecommendations), &lastRecommendations); err != nil {
			return SessionState{}, WrapError(err, "failed to parse session state")
		}
	}
	if lastRecommendations == nil {
		lastRecommendations = []int64{}
	}
	return SessionState{
		SessionID:           row.ID,
		UserID:              row.UserID,
		Phase:               ParsePhase(row.Phase),
		SourceMode:          sourceMode,
		CurrentIntent:       currentIntent,
		Slots:               NewSlotBundle(document.MealTime, document.Mood, document.Scene, document.HealthGoal, document.Cuisine, document.Taste, document.Convenience),
		LastRecommendations: lastRecommendations,
	}, nil
}

// stateToRow serializes a SessionState into a database row.
func stateToRow(state SessionState) (SessionRow, error) {
	document := slotsDocument{
		MealTime:    state.Slots.MealTime,
		Mood:        state.Slots.Mood,
		Scene:       state.Slots.Scene,
		HealthGoal:  state.Slots.HealthGoal,
		Cuisine:     state.Slots.Cuisine,
		Taste:       state.Slots.Taste,
		Convenience: state.Slots.Convenience,
	}
	if state.SourceMode != "" {
		mode := string(state.SourceMode)
		document.Meta.SourceMode = &mode
	}
	if state.CurrentIntent != "" {
		intent := string(state.CurrentIntent)
		document.Meta.CurrentIntent = &intent
	}
	slotsJSON, err := json.Marshal(document)
	if err != nil {
		return SessionRow{}, WrapError(err, "failed to serialize session state")
	}
	lastRecommendations := state.LastRecommendations
	if lastRecommendations == nil {
		lastRecommendations = []int64{}
	}
	recommendationsJSON, err := json.Marshal(lastRecommendations)
	if err != nil {
		return SessionRow{}, WrapError(err, "failed to serialize session state")
	}
	return SessionRow{
		ID:                  state.SessionID,
		UserID:              state.UserID,
		Phase:               string(state.Phase),
		Slots:               string(slotsJSON),
		LastRecommendations: string(recommendationsJSON),
	}, nil
}
