// src/wakey/internal/wakey/toxicity.go

package wakey

import (
	"context"
	"math"
	"time"

	"go.uber.org/zap"
)

type ToxicityChecker struct {
	db        *DB
	moderator *MessageModerator
	log       *zap.SugaredLogger
}

func NewToxicityChecker(db *DB, moderator *MessageModerator) *ToxicityChecker {
	return &ToxicityChecker{
		db:        db,
		moderator: moderator,
		log:       zap.L().Named("toxicity").Sugar(),
	}
}

func (tc *ToxicityChecker) Start() {
	go tc.processUnratedWishes()
}

func (tc *ToxicityChecker) processUnratedWishes() {
	tc.log.Info("Starting toxicity check for unrated wishes")

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

	ctx := context.Background()
	for _, wish := range wishes {
		// Create a timeout context for each wish
		wishCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		score, err := tc.moderator.CheckMessage(wishCtx, wish.Content)
		cancel() // Cancel the context after the check

		if err != nil {
			tc.log.Errorf("Failed to check toxicity for wish %d: %v", wish.ID, err)
			continue
		}

		// Convert score to int (0-100 range)
		toxicityScore := int(math.Round(score * 100))

		err = tc.db.UpdateWishToxicity(wish.ID, toxicityScore)
		if err != nil {
			tc.log.Errorf("Failed to update toxicity score for wish %d: %v", wish.ID, err)
			continue
		}

		tc.log.Debugf("Updated toxicity score for wish %d: %d", wish.ID, toxicityScore)

		// Add a small delay between requests to avoid overwhelming the API
		time.Sleep(100 * time.Millisecond)
	}

	tc.log.Info("Completed toxicity check for unrated wishes")
}
