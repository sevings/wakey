package wakey

import (
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("record not found")

type DB struct {
	db  *gorm.DB
	log *zap.SugaredLogger
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
	User      User `gorm:"foreignKey:UserID"`
	Content   string
	WakeAt    time.Time
	OfferedAt time.Time
}

type Wish struct {
	gorm.Model
	FromID  int64
	PlanID  uint
	Plan    Plan `gorm:"foreignKey:PlanID"`
	Content string
}

func LoadDatabase(path string) (*DB, bool) {
	log := zap.L().Named("db").Sugar()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	err = db.AutoMigrate(&User{}, &Plan{}, &Wish{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	return &DB{
		db:  db,
		log: log,
	}, true
}

func (db *DB) SaveUser(user *User) error {
	return db.db.Create(user).Error
}

func (db *DB) GetUser(userID int64) (*User, error) {
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

func (db *DB) SavePlan(plan *Plan) error {
	plan.OfferedAt = time.Time{}
	return db.db.Create(plan).Error
}

func (db *DB) GetLatestPlan(userID int64) (*Plan, error) {
	var plan Plan
	now := time.Now().UTC()
	result := db.db.Where("user_id = ?", userID).
		Where("wake_at > ?", now).
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

func (db *DB) CopyPlanForNextDay(userID int64) error {
	var latestPlan Plan
	result := db.db.Where("user_id = ?", userID).
		Order("wake_at DESC").
		Limit(1).
		Find(&latestPlan)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	newPlan := Plan{
		UserID:  userID,
		Content: latestPlan.Content,
		WakeAt:  latestPlan.WakeAt.Add(24 * time.Hour),
	}

	return db.db.Create(&newPlan).Error
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

func (db *DB) FindUserForWish(senderID int64) (*Plan, error) {
	var plan Plan
	now := time.Now().UTC()
	oneHourAgo := now.Add(-1 * time.Hour)

	result := db.db.
		Joins("User").
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
	return db.db.Create(wish).Error
}

func (db *DB) GetWishByID(wishID uint) (*Wish, error) {
	var wish Wish
	result := db.db.Where("id = ?", wishID).Limit(1).Find(&wish)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &wish, nil
}

func (db *DB) GetFutureWishes() ([]Wish, error) {
	var wishes []Wish
	now := time.Now().UTC()
	result := db.db.
		Joins("Plan").
		Where("Plan.wake_at > ?", now).
		Find(&wishes)
	if result.Error != nil {
		return nil, result.Error
	}
	return wishes, nil
}
