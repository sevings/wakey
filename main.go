package main

import (
	"os"
	"os/signal"
	"wakey/internal/wakey"

	"go.uber.org/zap"
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

	bot.Start()
	defer bot.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
}
