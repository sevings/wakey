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

	wishSched := wakey.NewSched(cfg.MaxJobs)
	wishSched.Start()
	defer wishSched.Stop()

	planSched := wakey.NewSched(cfg.MaxJobs)
	planSched.Start()
	defer planSched.Stop()

	bot, ok := wakey.NewBot(cfg, db, wishSched, planSched)
	if !ok {
		logger.Panic("can't create bot")
	}

	pref := tele.Settings{
		Token:   cfg.TgToken,
		Poller:  &tele.LongPoller{Timeout: 30 * time.Second},
		OnError: bot.LogError,
	}

	api, err := tele.NewBot(pref)
	if err != nil {
		logger.Panic(err)
	}

	bot.Start(api)
	defer bot.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
}
