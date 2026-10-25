package diet

import (
	"encoding/json"
	"sort"
	"strings"
)

// searchLimit bounds how many rows the DB recall returns before ranking.
const searchLimit = 50

// MealService owns meal CRUD and the JSON_OVERLAPS recall used by the
// recommendation pipeline.
type MealService struct {
	store       *Store
	slotOptions *SlotOptionService
}

// NewMealService builds the meal service.
func NewMealService(store *Store, slotOptions *SlotOptionService) *MealService {
	return &MealService{store: store, slotOptions: slotOptions}
}

// FindPersonalMeals lists the personal meals of one user.
func (s *MealService) FindPersonalMeals(userID int64) ([]MealItem, error) {
	rows, err := s.store.FindPersonalMeals(userID)
	if err != nil {
		return nil, err
	}
	return mapMealRows(rows), nil
}

// FindPublicMeals lists the public meal library.
func (s *MealService) FindPublicMeals() ([]MealItem, error) {
	rows, err := s.store.FindPublicMeals()
	if err != nil {
		return nil, err
	}
	return mapMealRows(rows), nil
}

// HasPersonalMeals is the pre-check that skips PERSONAL recommendation
// turns for users without a personal library.
func (s *MealService) HasPersonalMeals(userID int64) (bool, error) {
	count, err := s.store.CountPersonalMeals(userID)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreatePersonalMeal validates and inserts one personal meal.
func (s *MealService) CreatePersonalMeal(userID int64, request MealRequest) (MealItem, error) {
	if err := s.validateMealRequest(request); err != nil {
		return MealItem{}, err
	}
	row := mealRequestToRow(0, SourcePersonal, &userID, request)
	if err := s.store.InsertMeal(&row); err != nil {
		return MealItem{}, err
	}
	return mealRowToItem(row), nil
}

// UpdatePersonalMeal validates and updates one personal meal owned by the
// user.
func (s *MealService) UpdatePersonalMeal(userID, mealID int64, request MealRequest) (MealItem, error) {
	if err := s.validateMealRequest(request); err != nil {
		return MealItem{}, err
	}
	row := mealRequestToRow(mealID, SourcePersonal, &userID, request)
	updated, err := s.store.UpdatePersonalMeal(row)
	if err != nil {
		return MealItem{}, err
	}
	if updated == 0 {
		return MealItem{}, NewError("personal meal does not exist or you have no permission to modify it")
	}
	fresh, err := s.store.FindPersonalMealByID(mealID, userID)
	if err != nil {
		return MealItem{}, err
	}
	if fresh == nil {
		return MealItem{}, NewError("personal meal does not exist or you have no permission to modify it")
	}
	return mealRowToItem(*fresh), nil
}

// DeletePersonalMeal deletes one personal meal owned by the user.
func (s *MealService) DeletePersonalMeal(userID, mealID int64) error {
	deleted, err := s.store.DeletePersonalMeal(mealID, userID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return NewError("personal meal does not exist or you have no permission to delete it")
	}
	return nil
}

// Search recalls meal candidates by source mode and slots. Excluding
// previously recommended meals happens in the rank layer.
func (s *MealService) Search(sourceMode SourceMode, userID int64, slots SlotBundle) ([]MealItem, error) {
	rows, err := s.store.SearchMeals(sourceMode, userID, slots, searchLimit)
	if err != nil {
		return nil, err
	}
	return mapMealRows(rows), nil
}

func (s *MealService) validateMealRequest(request MealRequest) error {
	if strings.TrimSpace(request.Name) == "" {
		return NewError("meal name is required")
	}
	slots := request.ToSlots()
	if len(slots.MealTime) == 0 {
		return NewError("meal time needs at least one label")
	}
	return s.slotOptions.Validate(slots)
}

func mealRequestToRow(id int64, sourceMode SourceMode, ownerUserID *int64, request MealRequest) MealItemRow {
	slots := request.ToSlots()
	return MealItemRow{
		ID:          id,
		SourceType:  string(sourceMode),
		OwnerUserID: ownerUserID,
		Name:        strings.TrimSpace(request.Name),
		MealTime:    jsonStringArray(slots.MealTime),
		Mood:        jsonStringArray(slots.Mood),
		Scene:       jsonStringArray(slots.Scene),
		HealthGoal:  jsonStringArray(slots.HealthGoal),
		Cuisine:     jsonStringArray(slots.Cuisine),
		Taste:       jsonStringArray(slots.Taste),
		Convenience: jsonStringArray(slots.Convenience),
	}
}

func mapMealRows(rows []MealItemRow) []MealItem {
	items := make([]MealItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, mealRowToItem(row))
	}
	return items
}

func mealRowToItem(row MealItemRow) MealItem {
	sourceMode, _ := ParseSourceMode(row.SourceType)
	return MealItem{
		ID:          row.ID,
		SourceType:  sourceMode,
		OwnerUserID: row.OwnerUserID,
		Name:        row.Name,
		Slots: NewSlotBundle(
			parseStringArray(row.MealTime),
			parseStringArray(row.Mood),
			parseStringArray(row.Scene),
			parseStringArray(row.HealthGoal),
			parseStringArray(row.Cuisine),
			parseStringArray(row.Taste),
			parseStringArray(row.Convenience),
		),
		MatchScore: 0,
	}
}

func jsonStringArray(values []string) string {
	if values == nil {
		values = []string{}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func parseStringArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

// MealSearchService is the orchestrator-facing recall step with source
// isolation checks.
type MealSearchService struct {
	meals *MealService
}

// NewMealSearchService builds the search service.
func NewMealSearchService(meals *MealService) *MealSearchService {
	return &MealSearchService{meals: meals}
}

// Search recalls candidates for the given source mode and slots.
func (s *MealSearchService) Search(sourceMode SourceMode, userID int64, slots SlotBundle) ([]MealItem, error) {
	if sourceMode == "" {
		return nil, NewError("sourceMode is required")
	}
	if sourceMode == SourcePersonal && userID == 0 {
		return nil, NewError("userId is required in PERSONAL mode")
	}
	return s.meals.Search(sourceMode, userID, slots)
}

// rankLimit caps the number of candidates passed to the response agents.
const rankLimit = 10

// MealRankService re-scores recalled candidates by how well they satisfy
// the query slots, excluding previously recommended meals.
type MealRankService struct{}

// Rank filters excluded IDs, scores every candidate, and returns the top
// rankLimit items sorted by score descending.
func (MealRankService) Rank(candidates []MealItem, slots SlotBundle, excludeIDs []int64) []MealItem {
	excluded := make(map[int64]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		excluded[id] = struct{}{}
	}
	scored := make([]MealItem, 0, len(candidates))
	for _, item := range candidates {
		if item.ID == 0 {
			continue
		}
		if _, skip := excluded[item.ID]; skip {
			continue
		}
		item.MatchScore = slotScore(item.Slots, slots)
		scored = append(scored, item)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].MatchScore > scored[j].MatchScore
	})
	if len(scored) > rankLimit {
		scored = scored[:rankLimit]
	}
	return scored
}

// slotScore averages the overlap ratio of the seven slot dimensions.
func slotScore(item, query SlotBundle) float64 {
	total := overlapRatio(item.MealTime, query.MealTime) +
		overlapRatio(item.Mood, query.Mood) +
		overlapRatio(item.Scene, query.Scene) +
		overlapRatio(item.HealthGoal, query.HealthGoal) +
		overlapRatio(item.Cuisine, query.Cuisine) +
		overlapRatio(item.Taste, query.Taste) +
		overlapRatio(item.Convenience, query.Convenience)
	return clampScore(total / 7.0)
}

// overlapRatio is the share of query labels present on the item; an empty
// query dimension scores zero.
func overlapRatio(itemValues, queryValues []string) float64 {
	if len(queryValues) == 0 {
		return 0
	}
	itemSet := make(map[string]struct{}, len(itemValues))
	for _, value := range itemValues {
		itemSet[value] = struct{}{}
	}
	hits := 0
	for _, value := range queryValues {
		if _, ok := itemSet[value]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(queryValues))
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
