package wakey

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type ToxicityChecker struct {
	db     *DB
	moder  *MessageModerator
	log    *zap.SugaredLogger
	quit   chan struct{}
	wishCh <-chan *Wish
	unsub  func()
}

func NewToxicityChecker(db *DB, moderator *MessageModerator) *ToxicityChecker {
	wishChan, unsub := db.SubscribeToWishes(100)
	return &ToxicityChecker{
		db:     db,
		moder:  moderator,
		log:    zap.L().Named("toxicity").Sugar(),
		quit:   make(chan struct{}),
		wishCh: wishChan,
		unsub:  unsub,
	}
}

func (tc *ToxicityChecker) Start() {
	go tc.processUnratedWishes()
	go tc.processNewWishes()
}

func (tc *ToxicityChecker) Stop() {
	close(tc.quit)
	tc.unsub()
}

func (tc *ToxicityChecker) processNewWishes() {
	tc.log.Info("Started processing new wishes")

	for {
		select {
		case <-tc.quit:
			tc.log.Info("Stopping new wish processor")
			return
		case wish := <-tc.wishCh:
			tc.checkWishToxicity(wish)
		}
	}
}

func (tc *ToxicityChecker) processUnratedWishes() {
	tc.log.Info("Starting toxicity check for existing unrated wishes")

	wishes, err := tc.db.GetUnratedWishes()
	if err != nil {
		tc.log.Errorf("Failed to get unrated wishes: %v", err)
		return
	}

	if len(wishes) == 0 {
		tc.log.Info("No unrated wishes found")
		return
	}

	tc.log.Infof("Found %d unrated wishes to process", len(wishes))

	for _, wish := range wishes {
		select {
		case <-tc.quit:
			tc.log.Info("Stopping unrated wish processor")
			return
		default:
			tc.checkWishToxicity(&wish)
		}
	}

	tc.log.Info("Completed toxicity check for existing unrated wishes")
}

func (tc *ToxicityChecker) checkWishToxicity(wish *Wish) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tc.log.Debugf("Checking toxicity for wish %d", wish.ID)

	score, err := tc.moder.CheckMessage(ctx, wish.Content)
	if err != nil {
		tc.log.Errorf("Failed to check toxicity for wish %d: %v", wish.ID, err)
		return
	}

	toxicityScore := int16(score * 100)

	err = tc.db.UpdateWishToxicity(wish.ID, int(toxicityScore))
	if err != nil {
		tc.log.Errorf("Failed to update toxicity score for wish %d: %v", wish.ID, err)
		return
	}

	tc.log.Debugf("Updated toxicity score for wish %d: %d", wish.ID, toxicityScore)

	// Add a small delay between requests to avoid overwhelming the API
	time.Sleep(100 * time.Millisecond)
}
