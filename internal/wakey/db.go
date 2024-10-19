package wakey

import (
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type DB struct {
	db  *gorm.DB
	log *zap.SugaredLogger
}

type User struct {
	ID   int64
	Name string
	Bio  string
	Tz   int32
}

type Plan struct {
	UserID int64
	Plan   string
	WakeAt time.Time
}

type Wish struct {
	FromID int64
	ToID   int64
	Wish   string
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
