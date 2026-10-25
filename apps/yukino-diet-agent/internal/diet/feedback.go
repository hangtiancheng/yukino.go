package diet

import "strings"

// FeedbackService persists user feedback on recommendations.
type FeedbackService struct {
	store *Store
}

// NewFeedbackService builds the feedback service.
func NewFeedbackService(store *Store) *FeedbackService {
	return &FeedbackService{store: store}
}

// Save validates and inserts one feedback record.
func (s *FeedbackService) Save(userID int64, request FeedbackRequest) error {
	if strings.TrimSpace(request.SessionID) == "" {
		return NewError("feedback sessionId is required")
	}
	if strings.TrimSpace(request.Action) == "" {
		return NewError("feedback action is required")
	}
	return s.store.InsertFeedback(userID, request)
}
