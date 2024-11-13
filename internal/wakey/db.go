package wakey

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("record not found")

type DB struct {
	db        *gorm.DB
	log       *zap.SugaredLogger
	wishSubs  *SubscriptionManager
	toxicSubs *SubscriptionManager
	stateSubs *SubscriptionManager
}

type User struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	Name      string
	Bio       string
	Tz        int32
	IsBanned  bool
	NotifyAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

type Plan struct {
	gorm.Model
	UserID    int64
	Content   string
	WakeAt    time.Time
	OfferedAt time.Time
}

type WishState string

const (
	WishStateNew      WishState = "N"
	WishStateSent     WishState = "S"
	WishStateLiked    WishState = "L"
	WishStateDisliked WishState = "D"
	WishStateReported WishState = "R"
	WishStateBanned   WishState = "B"
)

type Wish struct {
	gorm.Model
	FromID   int64
	PlanID   uint
	Content  string
	State    WishState `gorm:"type:char(1);default:'N'"`
	Toxicity sql.NullInt16
}

type Stats struct {
	TotalUsers  int64
	TotalPlans  int64
	TotalWishes int64

	NewUsersLast7Days    int64
	ActiveUsersLast7Days int64

	AvgPlansLast7Days  float64
	AvgWishesLast7Days float64

	TotalLikedWishes            int64
	LikedWishesPercent          float64
	LikedWishesLast7Days        int64
	LikedWishesLast7DaysPercent float64
}

type State struct {
	UserID int64 `gorm:"primaryKey;autoIncrement:false"`
	UserData
}

func LoadDatabase(path string) (*DB, bool) {
	log := zap.L().Named("db").Sugar()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	err = db.AutoMigrate(&User{}, &Plan{}, &Wish{}, &State{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	return &DB{
		db:  db,
		log: log,

		wishSubs:  NewSubscriptionManager("wish", log),
		toxicSubs: NewSubscriptionManager("toxicity", log),
		stateSubs: NewSubscriptionManager("state", log),
	}, true
}

// Stop closes all subscription channels and performs cleanup
func (db *DB) Stop() {
	db.wishSubs.Close()
	db.toxicSubs.Close()
	db.stateSubs.Close()
}

// SubscribeToWishes returns a channel for wish notifications and an unsubscribe function
func (db *DB) SubscribeToWishes(bufSize int) (<-chan *Wish, func()) {
	return db.wishSubs.Subscribe(bufSize)
}

// SubscribeToToxicity returns a channel for wish toxicity update notifications and an unsubscribe function
func (db *DB) SubscribeToToxicity(bufSize int) (<-chan *Wish, func()) {
	return db.toxicSubs.Subscribe(bufSize)
}

// SubscribeToStateUpdates returns a channel for wish state update notifications and an unsubscribe function
func (db *DB) SubscribeToStateUpdates(bufSize int) (<-chan *Wish, func()) {
	return db.stateSubs.Subscribe(bufSize)
}

func (db *DB) GetStats() (*Stats, error) {
	stats := &Stats{}

	// Get total counts
	err := db.db.Model(&User{}).Count(&stats.TotalUsers).Error
	if err != nil {
		return nil, err
	}

	err = db.db.Model(&Plan{}).Count(&stats.TotalPlans).Error
	if err != nil {
		return nil, err
	}

	err = db.db.Model(&Wish{}).Count(&stats.TotalWishes).Error
	if err != nil {
		return nil, err
	}

	// Get new users in last 7 days
	sevenDaysAgo := time.Now().UTC().AddDate(0, 0, -7)
	err = db.db.Model(&User{}).
		Where("created_at >= ?", sevenDaysAgo).
		Count(&stats.NewUsersLast7Days).Error
	if err != nil {
		return nil, err
	}

	// Get active users (users with plans or wishes) in last 7 days
	var activeUsers int64
	err = db.db.Model(&User{}).
		Where("id IN (?)",
			db.db.Model(&Plan{}).
				Select("user_id").
				Where("created_at >= ?", sevenDaysAgo),
		).Or("id IN (?)",
		db.db.Model(&Wish{}).
			Select("from_id").
			Where("created_at >= ?", sevenDaysAgo),
	).Count(&activeUsers).Error
	if err != nil {
		return nil, err
	}
	stats.ActiveUsersLast7Days = activeUsers

	// Get average plans per day for last 7 days
	var plansLast7Days int64
	err = db.db.Model(&Plan{}).
		Where("created_at >= ?", sevenDaysAgo).
		Count(&plansLast7Days).Error
	if err != nil {
		return nil, err
	}
	stats.AvgPlansLast7Days = float64(plansLast7Days) / 7.0

	// Get average wishes per day for last 7 days
	var wishesLast7Days int64
	err = db.db.Model(&Wish{}).
		Where("created_at >= ?", sevenDaysAgo).
		Count(&wishesLast7Days).Error
	if err != nil {
		return nil, err
	}
	stats.AvgWishesLast7Days = float64(wishesLast7Days) / 7.0

	// Calculate total liked wishes and percentage
	var totalLikedWishes int64
	err = db.db.Model(&Wish{}).
		Where("state = ?", WishStateLiked).
		Count(&totalLikedWishes).Error
	if err != nil {
		return nil, err
	}
	stats.TotalLikedWishes = totalLikedWishes

	if stats.TotalWishes > 0 {
		stats.LikedWishesPercent = float64(totalLikedWishes) * 100.0 / float64(stats.TotalWishes)
	}

	// Calculate liked wishes in last 7 days and percentage
	var likedWishesLast7Days int64
	err = db.db.Model(&Wish{}).
		Where("state = ? AND created_at >= ?", WishStateLiked, sevenDaysAgo).
		Count(&likedWishesLast7Days).Error
	if err != nil {
		return nil, err
	}
	stats.LikedWishesLast7Days = likedWishesLast7Days

	var totalWishesLast7Days int64
	err = db.db.Model(&Wish{}).
		Where("created_at >= ?", sevenDaysAgo).
		Count(&totalWishesLast7Days).Error
	if err != nil {
		return nil, err
	}

	if totalWishesLast7Days > 0 {
		stats.LikedWishesLast7DaysPercent = float64(likedWishesLast7Days) * 100.0 / float64(totalWishesLast7Days)
	}

	return stats, nil
}

func (db *DB) CreateUser(user *User) error {
	result := db.db.Create(user)
	if result.Error != nil {
		if strings.Contains(result.Error.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("user with ID %d already exists", user.ID)
		}
		return result.Error
	}
	return nil
}

func (db *DB) SaveUser(user *User) error {
	result := db.db.Save(user)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) GetUserByID(userID int64) (*User, error) {
	var user User
	result := db.db.Where("id = ?", userID).Limit(1).Find(&user)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &user, nil
}

func (db *DB) GetAllUsers() ([]*User, error) {
	var users []*User
	result := db.db.Find(&users)
	if result.Error != nil {
		return nil, result.Error
	}
	return users, nil
}

// BanUser sets a user's IsBanned status to true and updates all their new wishes to banned state
func (db *DB) BanUser(userID int64) error {
	// Start a transaction
	tx := db.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Update user's banned status
	result := tx.Model(&User{}).Where("id = ?", userID).Update("is_banned", true)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return ErrNotFound
	}

	// Find all new wishes from this user and update their state to banned
	var wishes []Wish
	result = tx.Where("from_id = ? AND state = ?", userID, WishStateNew).Find(&wishes)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	for _, wish := range wishes {
		wish.State = WishStateBanned
		if err := tx.Save(&wish).Error; err != nil {
			tx.Rollback()
			return err
		}
		// Notify subscribers about state change
		db.stateSubs.Notify(&wish)
	}

	return tx.Commit().Error
}

func (db *DB) SavePlan(plan *Plan) error {
	plan.OfferedAt = time.Time{}
	return db.db.Save(plan).Error
}

func (db *DB) GetLatestPlan(userID int64) (*Plan, error) {
	var plan Plan
	result := db.db.Where("user_id = ?", userID).
		Order("wake_at DESC").
		Limit(1).
		Find(&plan)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &plan, nil
}

func (db *DB) CopyPlanForNextDay(userID int64) (*Plan, error) {
	var latestPlan Plan
	result := db.db.Where("user_id = ?", userID).
		Order("wake_at DESC").
		Limit(1).
		Find(&latestPlan)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}

	now := time.Now().UTC()
	if latestPlan.WakeAt.After(now) {
		return &latestPlan, nil
	}

	newPlan := Plan{
		UserID:  userID,
		Content: latestPlan.Content,
		WakeAt:  latestPlan.WakeAt,
	}

	for newPlan.WakeAt.Before(now) {
		newPlan.WakeAt = newPlan.WakeAt.Add(24 * time.Hour)
	}

	err := db.db.Create(&newPlan).Error
	if err != nil {
		return nil, err
	}

	return &newPlan, nil
}

func (db *DB) GetPlanByID(planID uint) (*Plan, error) {
	var plan Plan
	result := db.db.Where("id = ?", planID).Limit(1).Find(&plan)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &plan, nil
}

func (db *DB) GetAllPlansForUser(userID int64) ([]Plan, error) {
	var plans []Plan
	result := db.db.Where("user_id = ?", userID).
		Order("wake_at DESC").
		Find(&plans)
	if result.Error != nil {
		return nil, result.Error
	}
	return plans, nil
}

func (db *DB) FindPlanForWish(senderID int64) (*Plan, error) {
	var plan Plan
	now := time.Now().UTC()
	oneHourAgo := now.Add(-1 * time.Hour)

	result := db.db.
		Joins("LEFT JOIN wishes ON plans.id = wishes.plan_id").
		Where("plans.user_id != ?", senderID).
		Where("plans.wake_at > ?", now).
		Where("wishes.id IS NULL").
		Where("plans.offered_at < ?", oneHourAgo).
		Order("RANDOM()").
		Limit(1).
		Find(&plan)

	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}

	plan.OfferedAt = now
	db.db.Save(&plan)

	return &plan, nil
}

func (db *DB) SaveWish(wish *Wish) error {
	err := db.db.Create(wish).Error
	if err != nil {
		return err
	}

	db.wishSubs.Notify(wish)

	return nil
}

func (db *DB) GetWishByID(wishID uint) (*Wish, error) {
	var wish Wish
	result := db.db.
		Where("wishes.id = ?", wishID).
		Limit(1).
		Find(&wish)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &wish, nil
}

func (db *DB) GetNewWishesByUserID(userID int64) ([]Wish, error) {
	var wishes []Wish
	result := db.db.
		Joins("JOIN plans ON wishes.plan_id = plans.id").
		Where("plans.user_id = ? AND wishes.state = ?", userID, WishStateNew).
		Find(&wishes)

	if result.Error != nil {
		return nil, result.Error
	}
	return wishes, nil
}

func (db *DB) UpdateWishState(wishID uint, state WishState) error {
	// First get the wish to send in notification
	var wish Wish
	result := db.db.Where("id = ?", wishID).First(&wish)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return ErrNotFound
		}
		return result.Error
	}

	// Update state
	result = db.db.Model(&wish).Update("state", state)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	// Update the wish object with new state
	wish.State = state
	db.stateSubs.Notify(&wish)

	return nil
}

// GetUnratedWishes returns all wishes where toxicity is not set (equals 0)
func (db *DB) GetUnratedWishes() ([]Wish, error) {
	var wishes []Wish
	result := db.db.Where("toxicity IS NULL").Find(&wishes)
	if result.Error != nil {
		return nil, result.Error
	}
	return wishes, nil
}

// UpdateWishToxicity updates the toxicity score for a specific wish
func (db *DB) UpdateWishToxicity(wishID uint, toxicity int) error {
	// First get the wish to send in notification
	var wish Wish
	result := db.db.Where("id = ?", wishID).First(&wish)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return ErrNotFound
		}
		return result.Error
	}

	// Update toxicity
	result = db.db.Model(&wish).Update("toxicity", toxicity)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	// Update the wish object with new toxicity value
	wish.Toxicity = sql.NullInt16{Int16: int16(toxicity), Valid: true}
	db.toxicSubs.Notify(&wish)

	return nil
}

func (db *DB) GetFuturePlans() ([]Plan, error) {
	var plans []Plan
	now := time.Now().UTC()
	result := db.db.
		Where("wake_at > ?", now).
		Find(&plans)
	if result.Error != nil {
		return nil, result.Error
	}
	return plans, nil
}

func (db *DB) SaveStates(states map[int64]*UserData) error {
	// First, clear existing states
	if err := db.db.Where("1 = 1").Delete(&State{}).Error; err != nil {
		return fmt.Errorf("failed to clear states: %w", err)
	}

	// Create new states
	for userID, userData := range states {
		state := State{
			UserID:   userID,
			UserData: *userData,
		}

		if err := db.db.Save(&state).Error; err != nil {
			return fmt.Errorf("failed to save state for user %d: %w", userID, err)
		}
	}

	return nil
}

func (db *DB) LoadStates() (map[int64]*UserData, error) {
	var states []State
	if err := db.db.Find(&states).Error; err != nil {
		return nil, fmt.Errorf("failed to load states: %w", err)
	}

	result := make(map[int64]*UserData)
	for _, state := range states {
		userData := state.UserData
		result[state.UserID] = &userData
	}

	return result, nil
}
