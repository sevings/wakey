package wakey

import (
	"sync"

	"go.uber.org/zap"
)

// SubscriptionManager handles channel subscriptions and notifications
type SubscriptionManager struct {
	subs     map[int]chan *Wish
	subMutex sync.RWMutex
	nextID   int
	log      *zap.SugaredLogger
	name     string
}

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager(name string, log *zap.SugaredLogger) *SubscriptionManager {
	return &SubscriptionManager{
		subs: make(map[int]chan *Wish),
		log:  log,
		name: name,
	}
}

// Subscribe returns a channel for notifications and an unsubscribe function
func (sm *SubscriptionManager) Subscribe(bufSize int) (<-chan *Wish, func()) {
	sm.subMutex.Lock()
	defer sm.subMutex.Unlock()

	id := sm.nextID
	sm.nextID++

	ch := make(chan *Wish, bufSize)
	sm.subs[id] = ch

	unsubscribe := func() {
		sm.subMutex.Lock()
		defer sm.subMutex.Unlock()

		if ch, ok := sm.subs[id]; ok {
			delete(sm.subs, id)
			close(ch)
		}
	}

	return ch, unsubscribe
}

// Notify sends a wish to all subscribers
func (sm *SubscriptionManager) Notify(wish *Wish) {
	sm.subMutex.RLock()
	defer sm.subMutex.RUnlock()

	for id, ch := range sm.subs {
		select {
		case ch <- wish:
			sm.log.Debugf("Notified %s subscriber %d about wish %d", sm.name, id, wish.ID)
		default:
			sm.log.Warnf("%s subscriber %d's channel is full, skipping notification for wish %d",
				sm.name, id, wish.ID)
		}
	}
}

// Close closes all subscription channels
func (sm *SubscriptionManager) Close() {
	sm.subMutex.Lock()
	defer sm.subMutex.Unlock()

	for id, ch := range sm.subs {
		close(ch)
		delete(sm.subs, id)
	}
}
