package diet

// SlotMergeService merges history slots with the slots of the current
// turn: non-empty current values win, empty current values keep history.
type SlotMergeService struct{}

// Merge combines two slot bundles dimension by dimension.
func (SlotMergeService) Merge(history, current SlotBundle) SlotBundle {
	return NewSlotBundle(
		chooseLabels(history.MealTime, current.MealTime),
		chooseLabels(history.Mood, current.Mood),
		chooseLabels(history.Scene, current.Scene),
		chooseLabels(history.HealthGoal, current.HealthGoal),
		chooseLabels(history.Cuisine, current.Cuisine),
		chooseLabels(history.Taste, current.Taste),
		chooseLabels(history.Convenience, current.Convenience),
	)
}

func chooseLabels(history, current []string) []string {
	if len(current) == 0 {
		return history
	}
	return current
}

// SlotOptionService exposes the slot dictionary stored in
// diet_slot_option and validates labels against it.
type SlotOptionService struct {
	store *Store
}

// NewSlotOptionService builds the slot dictionary service.
func NewSlotOptionService(store *Store) *SlotOptionService {
	return &SlotOptionService{store: store}
}

// FindAllOptions loads every enabled option value keyed by slot name, in
// the canonical slot order.
func (s *SlotOptionService) FindAllOptions() (map[string][]string, error) {
	options := make(map[string][]string, len(SlotNames))
	for _, slotName := range SlotNames {
		values, err := s.store.FindEnabledSlotValues(slotName)
		if err != nil {
			return nil, err
		}
		if values == nil {
			values = []string{}
		}
		options[slotName] = values
	}
	return options, nil
}

// Validate rejects slot labels that are not in the dictionary.
func (s *SlotOptionService) Validate(slots SlotBundle) error {
	options, err := s.FindAllOptions()
	if err != nil {
		return err
	}
	for _, slotName := range SlotNames {
		allowed := options[slotName]
		for _, value := range slots.Get(slotName) {
			if !containsString(allowed, value) {
				return NewError("illegal slot label: %s=%s", slotName, value)
			}
		}
	}
	return nil
}
