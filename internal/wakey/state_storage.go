package wakey

import "go.uber.org/zap"

type StateStorage struct {
	db     *DB
	logger *zap.SugaredLogger
}

func NewStateStorage(db *DB) *StateStorage {
	return &StateStorage{
		db:     db,
		logger: zap.L().Named("state_storage").Sugar(),
	}
}

func (s *StateStorage) LoadToManager(manager *StateManager) {
	savedStates, err := s.db.LoadStates()
	if err != nil {
		s.logger.Errorf("Failed to load states from database: %v", err)
		return
	}

	err = manager.ImportStates(savedStates)
	if err != nil {
		s.logger.Errorf("Failed to import states: %v", err)
		return
	}

	s.logger.Infof("Loaded %d states from database", len(savedStates))
}

func (s *StateStorage) SaveFromManager(manager *StateManager) {
	states, err := manager.ExportStates()
	if err != nil {
		s.logger.Errorf("Failed to export states: %v", err)
		return
	}

	err = s.db.SaveStates(states)
	if err != nil {
		s.logger.Errorf("Failed to save states to database: %v", err)
		return
	}

	s.logger.Infof("Saved %d states to database", len(states))
}
