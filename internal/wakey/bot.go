package wakey

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
)

type Bot struct {
	bot *tele.Bot
	db  *DB
	adm int64
	log *zap.SugaredLogger
}

func NewBot(cfg Config, db *DB) (*Bot, bool) {
	bot := &Bot{
		db:  db,
		adm: cfg.AdminID,
		log: zap.L().Named("bot").Sugar(),
	}

	pref := tele.Settings{
		Token:   cfg.TgToken,
		Poller:  &tele.LongPoller{Timeout: 30 * time.Second},
		OnError: bot.logError,
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		bot.log.Error(err)
		return nil, false
	}
	bot.bot = b

	bot.bot.Use(middleware.Recover())
	bot.bot.Use(bot.logCmd)

	return bot, true
}

func (bot *Bot) Start() {
	go func() {
		bot.log.Info("starting bot")
		bot.bot.Start()
		bot.log.Info("bot stopped")
	}()
}

func (bot *Bot) Stop() {
	bot.bot.Stop()
}

func (bot *Bot) logMessage(c tele.Context, beginTime int64, err error) {
	endTime := time.Now().UnixNano()
	duration := float64(endTime-beginTime) / 1000000

	isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
	var cmd string
	if isCmd {
		cmd = c.Text()
	}
	bot.log.Infow("user message",
		"chat_id", c.Chat().ID,
		"chat_type", c.Chat().Type,
		"user_id", c.Sender().ID,
		"user_name", c.Sender().Username,
		"is_cmd", isCmd,
		"cmd", cmd,
		"size", len(c.Text()),
		"dur", fmt.Sprintf("%.2f", duration),
		"err", err)
}

func (bot *Bot) logCmd(next tele.HandlerFunc) tele.HandlerFunc {
	mention := "@" + bot.bot.Me.Username

	return func(c tele.Context) error {
		beginTime := time.Now().UnixNano()
		isBotCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1 &&
			(c.Chat().Type == tele.ChatPrivate ||
				strings.Contains(c.Text(), mention) ||
				!strings.Contains(c.Text(), "@"))

		err := next(c)

		if isBotCmd {
			bot.logMessage(c, beginTime, err)
		}

		return err
	}
}

func (bot *Bot) logError(err error, c tele.Context) {
	if c == nil {
		bot.log.Errorw("error", "err", err)
	} else {
		isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		var cmd string
		if isCmd {
			cmd = c.Text()
			idx := strings.Index(cmd, " ")
			if idx > 0 {
				cmd = cmd[:idx]
			}
		}
		bot.log.Errorw("error",
			"chat_id", c.Chat().ID,
			"chat_type", c.Chat().Type,
			"user_id", c.Sender().ID,
			"user_name", c.Sender().Username,
			"is_cmd", isCmd,
			"cmd", cmd,
			"size", len(c.Text()),
			"err", err)
	}
}
