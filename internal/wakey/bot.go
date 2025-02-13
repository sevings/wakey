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
	api            BotAPI
	db             *DB
	stateManager   *StateManager
	actionHandlers map[string]BotHandler
	stateHandlers  map[UserState]BotHandler
	log            *zap.SugaredLogger
}

type BotAPI interface {
	Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error)
	Handle(endpoint interface{}, h tele.HandlerFunc, m ...tele.MiddlewareFunc)
	Use(middlewares ...tele.MiddlewareFunc)
	Start()
	Stop()
}

type BotHandler interface {
	Actions() []string
	HandleAction(c tele.Context, action string) error
	States() []UserState
	HandleState(c tele.Context, state UserState) error
}

type JobID int64
type JobFunc func(JobID)

type Scheduler interface {
	SetJobFunc(fn JobFunc)
	Schedule(at time.Time, id JobID)
	Cancel(id JobID)
}

const (
	btnWishLikeID         = "wish_like"
	btnWishDislikeID      = "wish_dislike"
	btnWishReportID       = "wish_report"
	btnSendWishYesID      = "send_wish_yes"
	btnSendWishNoID       = "send_wish_no"
	btnKeepPlansID        = "keep_plans"
	btnUpdatePlansID      = "update_plans"
	btnNoWishID           = "no_wish"
	btnShowProfileID      = "show_profile"
	btnChangeNameID       = "change_name"
	btnChangeBioID        = "change_bio"
	btnChangeTimezoneID   = "change_timezone"
	btnChangePlansID      = "change_plans"
	btnChangeWakeTimeID   = "change_wake_time"
	btnChangeNotifyTimeID = "change_notify_time"
	btnInviteFriendsID    = "invite_friends"
	btnDoNothingID        = "do_nothing"
	btnShowLinkID         = "show_link"
	btnWarnUserID         = "warn_user"
	btnBanUserID          = "ban_user"
	btnSkipBanID          = "skip_ban"
)

const (
	btnWishLikeText         = "♥ Спасибо, приятно!"
	btnWishDislikeText      = "😐 Ну такое…"
	btnWishReportText       = "🙎 Это даже обидно"
	btnSendWishYesText      = "💌 Отправить сообщение"
	btnSendWishNoText       = "❌ Не сейчас"
	btnKeepPlansText        = "👌 Оставить как есть"
	btnUpdatePlansText      = "✍ Изменить статус и время"
	btnNoWishText           = "🚫 Не получать сообщение"
	btnShowProfileText      = "👤 Показать мой профиль"
	btnChangeNameText       = "📝 Изменить имя"
	btnChangeBioText        = "📋 Изменить био"
	btnChangeTimezoneText   = "🌍 Изменить часовой пояс"
	btnChangePlansText      = "✍ Изменить статус"
	btnChangeWakeTimeText   = "⏰ Изменить время пробуждения"
	btnChangeNotifyTimeText = "🔔 Изменить время уведомления"
	btnInviteFriendsText    = "👥 Пригласить друзей"
	btnDoNothingText        = "🤷‍♂️ Ничего, до свидания"
	btnShowLinkText         = "🔗 Показать ссылку"
	btnShareLinkText        = "📤 Поделиться ссылкой"
	btnWarnUserText         = "⚠️ Отправить предупреждение"
	btnBanUserText          = "🚫 Забанить пользователя"
	btnSkipBanText          = "⏭️ Пропустить"
)

var btnTextMap = map[string]string{
	btnWishLikeID:         btnWishLikeText,
	btnWishDislikeID:      btnWishDislikeText,
	btnWishReportID:       btnWishReportText,
	btnSendWishYesID:      btnSendWishYesText,
	btnSendWishNoID:       btnSendWishNoText,
	btnKeepPlansID:        btnKeepPlansText,
	btnUpdatePlansID:      btnUpdatePlansText,
	btnNoWishID:           btnNoWishText,
	btnShowProfileID:      btnShowProfileText,
	btnChangeNameID:       btnChangeNameText,
	btnChangeBioID:        btnChangeBioText,
	btnChangeTimezoneID:   btnChangeTimezoneText,
	btnChangePlansID:      btnChangePlansText,
	btnChangeWakeTimeID:   btnChangeWakeTimeText,
	btnChangeNotifyTimeID: btnChangeNotifyTimeText,
	btnInviteFriendsID:    btnInviteFriendsText,
	btnDoNothingID:        btnDoNothingText,
	btnShowLinkID:         btnShowLinkText,
	btnWarnUserID:         btnWarnUserText,
	btnBanUserID:          btnBanUserText,
	btnSkipBanID:          btnSkipBanText,
}

func NewBot(db *DB, stateMan *StateManager) *Bot {
	bot := &Bot{
		db:             db,
		stateManager:   stateMan,
		log:            zap.L().Named("bot").Sugar(),
		actionHandlers: make(map[string]BotHandler),
		stateHandlers:  make(map[UserState]BotHandler),
	}

	return bot
}

func (bot *Bot) Logger() *zap.SugaredLogger {
	return bot.log
}

func (bot *Bot) Start(cfg Config, api BotAPI, handlers []BotHandler) {
	bot.api = api

	for _, handler := range handlers {
		for _, action := range handler.Actions() {
			bot.actionHandlers[action] = handler
		}
		for _, state := range handler.States() {
			bot.stateHandlers[state] = handler
		}
	}

	bot.api.Use(middleware.Recover())
	bot.api.Use(bot.logMessage)
	bot.api.Use(bot.checkBan)

	bot.api.Handle(tele.OnCallback, bot.handleCallback)
	bot.api.Handle(tele.OnText, bot.handleText)
	bot.api.Handle("/start", bot.handleStart)
	bot.api.Handle("/cancel", bot.handleCancel)
	bot.api.Handle("/stat", bot.handleStats)
	bot.api.Handle("/notify", bot.handleNotify)

	go func() {
		bot.log.Info("starting bot")
		bot.api.Start()
		bot.log.Info("bot stopped")
	}()
}

func (bot *Bot) Stop() {
	bot.api.Stop()
}

func (bot *Bot) logMessage(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		beginTime := time.Now().UnixNano()

		err := next(c)

		endTime := time.Now().UnixNano()
		duration := float64(endTime-beginTime) / 1000000

		isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		isAction := c.Callback() != nil
		var action string
		if isCmd {
			action = c.Text()
		} else if isAction {
			action = strings.TrimSpace(strings.Split(c.Callback().Data, "|")[0])
		}
		bot.log.Infow("user message",
			"chat_id", c.Chat().ID,
			"chat_type", c.Chat().Type,
			"user_id", c.Sender().ID,
			"user_name", c.Sender().Username,
			"is_cmd", isCmd,
			"is_action", isAction,
			"action", action,
			"size", len(c.Text()),
			"dur", fmt.Sprintf("%.2f", duration),
			"err", err)

		return err
	}
}

func (bot *Bot) LogError(err error, c tele.Context) {
	if c == nil {
		bot.log.Errorw("error", "err", err)
	} else {
		isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		isAction := c.Callback() != nil
		var action string
		if isCmd {
			action = c.Text()
			idx := strings.Index(action, " ")
			if idx > 0 {
				action = action[:idx]
			}
		} else if isAction {
			action = strings.TrimSpace(strings.Split(c.Callback().Data, "|")[0])
		}
		bot.log.Errorw("error",
			"chat_id", c.Chat().ID,
			"chat_type", c.Chat().Type,
			"user_id", c.Sender().ID,
			"user_name", c.Sender().Username,
			"is_cmd", isCmd,
			"is_action", isAction,
			"action", action,
			"size", len(c.Text()),
			"err", err)
	}
}

func (bot *Bot) checkBan(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		userID := c.Sender().ID

		// Check if user exists and is banned
		user, err := bot.db.GetUserByID(userID)
		if err == nil && user.IsBanned {
			const msg = "Извините, вы не можете использовать бота, так как были забанены."
			// Check if it's a callback query
			if c.Callback() != nil {
				return c.Respond(&tele.CallbackResponse{
					Text:      msg,
					ShowAlert: true,
				})
			}
			// For regular messages
			return c.Send(msg)
		}

		// If the user is not banned or doesn't exist, continue to the next handler
		return next(c)
	}
}

func (bot *Bot) handleCallback(c tele.Context) error {
	data := strings.Split(c.Data(), "|")
	action := strings.TrimSpace(data[0])

	handler, exists := bot.actionHandlers[action]
	if !exists {
		bot.log.Warnw("no handler for action", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}

	// Add button text to the message if it exists in the map
	if btnText, ok := btnTextMap[action]; ok {
		msg := fmt.Sprintf("%s\n\n%s", c.Message().Text, btnText)
		err := c.Edit(msg)
		if err != nil {
			bot.log.Warnw("failed to edit message with button text", "err", err)
		}
	}

	err := handler.HandleAction(c, action)
	if err != nil {
		return err
	}

	userID := c.Sender().ID
	state, exists := bot.stateManager.GetState(userID)
	if exists && state == StateSuggestActions {
		bot.stateManager.ClearState(userID)
		return bot.handleState(c, state)
	}

	return nil
}

func (bot *Bot) handleText(c tele.Context) error {
	userID := c.Sender().ID

	state, exists := bot.stateManager.GetState(userID)
	if !exists {
		state = StateSuggestActions
	}

	return bot.handleState(c, state)
}

func (bot *Bot) handleState(c tele.Context, state UserState) error {
	userID := c.Sender().ID

	handler, exists := bot.stateHandlers[state]
	if !exists {
		bot.log.Warnw("no handler for state", "state", state)
		return nil
	}

	err := handler.HandleState(c, state)
	if err != nil {
		return err
	}

	state, exists = bot.stateManager.GetState(userID)
	if exists && state == StateSuggestActions {
		bot.stateManager.ClearState(userID)
		return bot.handleState(c, state)
	}

	return nil
}

func (bot *Bot) handleStart(c tele.Context) error {
	return bot.handleState(c, StateRegistrationStart)
}

func (bot *Bot) handleCancel(c tele.Context) error {
	return bot.handleState(c, StateCancelAction)
}

func (bot *Bot) handleStats(c tele.Context) error {
	return bot.handleState(c, StatePrintStats)
}

func (bot *Bot) handleNotify(c tele.Context) error {
	return bot.handleState(c, StateNotifyAll)
}

func parseTime(timeStr string, userTz int32) (time.Time, error) {
	// Parse the time
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("Неверный формат времени. Пожалуйста, используйте формат ЧЧ:ММ (например, 14:30)")
	}

	// Create a time.Location using the user's timezone offset
	userLoc := time.FixedZone("User Timezone", int(userTz)*60)

	// Set the time to today in the user's timezone
	now := time.Now().In(userLoc)
	userTime := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, userLoc)

	// If the resulting time is in the past, assume it's for tomorrow
	for userTime.Before(now) {
		userTime = userTime.Add(24 * time.Hour)
	}

	// Convert to UTC
	return userTime.UTC(), nil
}
