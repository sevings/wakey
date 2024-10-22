package wakey

import (
	"sync"
	"time"
)

type UserState int

const (
	StateNone UserState = iota
	StateRegistrationStart
	StateAwaitingName
	StateAwaitingBio
	StateAwaitingTime
	StateAwaitingPlans
	StateAwaitingWakeTime
	StateAwaitingWish
	StateAwaitingNotificationTime
	StateUpdatingName
	StateUpdatingBio
	StateUpdatingTimezone
	StateUpdatingPlans
	StateUpdatingWakeTime
	StateUpdatingNotificationTime
)

type UserData struct {
	State          UserState
	IsSingleAction bool
	Name           string
	Bio            string
	Plans          string
	TargetUserID   int64
	TargetPlanID   uint
	LastUpdated    time.Time
}

type StateManager struct {
	states    map[int64]*UserData
	mutex     sync.RWMutex
	ticker    *time.Ticker
	done      chan struct{}
	stopOnce  sync.Once
	isStopped bool
	now       func() time.Time
}

func NewStateManager() *StateManager {
	return &StateManager{
		states: make(map[int64]*UserData),
		done:   make(chan struct{}),
		now:    time.Now,
	}
}

func (sm *StateManager) SetNowFunc(nowFunc func() time.Time) {
	sm.now = nowFunc
}

func (sm *StateManager) Start(cleanupInterval, maxAge time.Duration) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.isStopped {
		return
	}

	cleanupInterval = max(cleanupInterval, time.Hour)
	sm.ticker = time.NewTicker(cleanupInterval)
	go sm.cleanupRoutine(maxAge)
}

func (sm *StateManager) Stop() {
	sm.stopOnce.Do(func() {
		sm.mutex.Lock()
		defer sm.mutex.Unlock()

		if sm.ticker != nil {
			sm.ticker.Stop()
		}
		close(sm.done)
		sm.isStopped = true
	})
}

func (sm *StateManager) SetState(userID int64, state UserState) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	userData, exists := sm.states[userID]
	if !exists {
		userData = &UserData{}
		sm.states[userID] = userData
	}

	userData.State = state
	userData.LastUpdated = sm.now()
}

func (sm *StateManager) GetState(userID int64) (UserState, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	userData, exists := sm.states[userID]
	if !exists {
		return StateNone, false
	}

	return userData.State, true
}

func (sm *StateManager) SetUserData(userID int64, data *UserData) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	data.LastUpdated = sm.now()
	sm.states[userID] = data
}

func (sm *StateManager) GetUserData(userID int64) (*UserData, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	userData, exists := sm.states[userID]
	return userData, exists
}

func (sm *StateManager) ClearState(userID int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	delete(sm.states, userID)
}

func (sm *StateManager) cleanupRoutine(maxAge time.Duration) {
	for {
		select {
		case <-sm.done:
			return
		case <-sm.ticker.C:
			sm.CleanupOldStates(maxAge)
		}
	}
}

func (sm *StateManager) CleanupOldStates(maxAge time.Duration) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	now := sm.now()
	for userID, userData := range sm.states {
		if now.Sub(userData.LastUpdated) > maxAge {
			delete(sm.states, userID)
		}
	}
}
