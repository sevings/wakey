package main

import (
	"os"
	"os/signal"
	"time"
	"wakey/internal/wakey"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

func main() {
	cfg, err := wakey.LoadConfig()
	if err != nil {
		panic(err)
	}

	var zapLogger *zap.Logger
	if cfg.Release {
		zapLogger, err = zap.NewProduction(zap.WithCaller(false))
	} else {
		zapLogger, err = zap.NewDevelopment(zap.WithCaller(false))
	}
	if err != nil {
		panic(err)
	}
	defer func() { _ = zapLogger.Sync() }()

	zap.ReplaceGlobals(zapLogger)
	zap.RedirectStdLog(zapLogger)
	logger := zapLogger.Sugar()

	db, ok := wakey.LoadDatabase(cfg.DBPath)
	if !ok {
		logger.Panic("can't load database")
	}

	moderator, err := wakey.NewMessageModerator(cfg.Moderation)
	if err != nil {
		logger.Panicf("Failed to initialize message moderator: %v", err)
	}

	toxicityChecker := wakey.NewToxicityChecker(db, moderator)
	toxicityChecker.Start()
	defer toxicityChecker.Stop()

	wishSched := wakey.NewSched(cfg.MaxJobs)
	wishSched.Start()
	defer wishSched.Stop()

	planSched := wakey.NewSched(cfg.MaxJobs)
	planSched.Start()
	defer planSched.Stop()

	stateMan := wakey.NewStateManager()
	stateStorage := wakey.NewStateStorage(db)
	stateStorage.LoadToManager(stateMan)
	defer stateStorage.SaveFromManager(stateMan)

	maxStateAge := time.Duration(cfg.MaxStateAge) * time.Hour
	cleanupInterval := maxStateAge / 10
	stateMan.Start(cleanupInterval, maxStateAge)
	defer stateMan.Stop()

	bot := wakey.NewBot(db, stateMan)

	pref := tele.Settings{
		Token:   cfg.TgToken,
		Poller:  &tele.LongPoller{Timeout: 30 * time.Second},
		OnError: bot.LogError,
	}

	api, err := tele.NewBot(pref)
	if err != nil {
		logger.Panic(err)
	}

	planHandler := wakey.NewPlanHandler(db, api, planSched, wishSched, stateMan, bot.Logger())
	wishHandler := wakey.NewWishHandler(db, api, wishSched, stateMan, bot.Logger(), cfg.AdminID)
	profileHandler := wakey.NewProfileHandler(db, stateMan, bot.Logger())
	adminHandler := wakey.NewAdminHandler(db, api, stateMan, bot.Logger(), cfg.AdminID)
	generalHandler := wakey.NewGeneralHandler(db, stateMan, bot.Logger(), api.Me.Username)
	handlers := []wakey.BotHandler{planHandler, wishHandler, profileHandler, adminHandler, generalHandler}

	bot.Start(cfg, api, handlers)
	defer bot.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
}
