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
	handlers       []BotHandler
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

type APISetter interface {
	SetAPI(api BotAPI)
}

type AdminSetter interface {
	SetAdminID(adminID int64)
}

type JobID int64
type JobFunc func(JobID)

type Scheduler interface {
	SetJobFunc(fn JobFunc)
	Schedule(at time.Time, id JobID)
	Cancel(id JobID)
}

const (
	btnWishLike         = "wish_like"
	btnWishDislike      = "wish_dislike"
	btnWishReport       = "wish_report"
	btnSendWishYes      = "send_wish_yes"
	btnSendWishNo       = "send_wish_no"
	btnKeepPlans        = "keep_plans"
	btnUpdatePlans      = "update_plans"
	btnNoWish           = "no_wish"
	btnShowProfile      = "show_profile"
	btnChangeName       = "change_name"
	btnChangeBio        = "change_bio"
	btnChangeTimezone   = "change_timezone"
	btnChangePlans      = "change_plans"
	btnChangeWakeTime   = "change_wake_time"
	btnChangeNotifyTime = "change_notify_time"
	btnInviteFriends    = "invite_friends"
	btnDoNothing        = "do_nothing"
	btnShowLink         = "show_link"
	btnBanUser          = "ban_user"
	btnSkipBan          = "skip_ban"
)

func NewBot(db *DB) *Bot {
	bot := &Bot{
		db:             db,
		stateManager:   NewStateManager(),
		log:            zap.L().Named("bot").Sugar(),
		actionHandlers: make(map[string]BotHandler),
		stateHandlers:  make(map[UserState]BotHandler),
	}

	return bot
}

func (bot *Bot) Start(cfg Config, api BotAPI, wishSched, planSched Scheduler, botName string) {
	bot.api = api

	planHandler := NewPlanHandler(bot.db, planSched, bot.stateManager, bot.log)
	wishHandler := NewWishHandler(bot.db, wishSched, bot.stateManager, bot.log)
	profileHandler := NewProfileHandler(bot.db, bot.stateManager, bot.log)
	adminHandler := NewAdminHandler(bot.db, bot.log)
	generalHandler := NewGeneralHandler(bot.db, bot.log, botName)
	bot.handlers = []BotHandler{planHandler, wishHandler, profileHandler, adminHandler, generalHandler}

	for _, handler := range bot.handlers {
		for _, action := range handler.Actions() {
			bot.actionHandlers[action] = handler
		}
		for _, state := range handler.States() {
			bot.stateHandlers[state] = handler
		}
	}

	for _, handler := range bot.handlers {
		if apiSetter, ok := handler.(APISetter); ok {
			apiSetter.SetAPI(api)
		}
	}

	for _, handler := range bot.handlers {
		if adminSetter, ok := handler.(AdminSetter); ok {
			adminSetter.SetAdminID(cfg.AdminID)
		}
	}

	bot.api.Use(middleware.Recover())
	bot.api.Use(bot.logCmd)
	bot.api.Use(bot.checkBan)

	bot.api.Handle(tele.OnCallback, bot.handleCallback)
	bot.api.Handle(tele.OnText, bot.handleText)
	bot.api.Handle("/start", bot.handleStart)

	// Start the state manager cleanup routine
	cleanupInterval := time.Duration(cfg.MaxStateAge) / 10 * time.Hour
	maxStateAge := time.Duration(cfg.MaxStateAge) * time.Hour
	bot.stateManager.Start(cleanupInterval, maxStateAge)

	go func() {
		bot.log.Info("starting bot")
		bot.api.Start()
		bot.log.Info("bot stopped")
	}()
}

func (bot *Bot) Stop() {
	bot.stateManager.Stop()
	bot.api.Stop()
}

func (bot *Bot) logMessage(c tele.Context, beginTime int64, err error) {
	endTime := time.Now().UnixNano()
	duration := float64(endTime-beginTime) / 1000000

	isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
	isAction := c.Callback() != nil
	var cmd string
	if isCmd {
		cmd = c.Text()
	} else if isAction {
		cmd = strings.TrimSpace(strings.Split(c.Callback().Data, "|")[0])
	}
	bot.log.Infow("user message",
		"chat_id", c.Chat().ID,
		"chat_type", c.Chat().Type,
		"user_id", c.Sender().ID,
		"user_name", c.Sender().Username,
		"is_cmd", isCmd,
		"is_action", isAction,
		"cmd", cmd,
		"size", len(c.Text()),
		"dur", fmt.Sprintf("%.2f", duration),
		"err", err)
}

func (bot *Bot) logCmd(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		beginTime := time.Now().UnixNano()
		isBotCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		isAction := c.Callback() != nil

		err := next(c)

		if isBotCmd || isAction {
			bot.logMessage(c, beginTime, err)
		}

		return err
	}
}

func (bot *Bot) LogError(err error, c tele.Context) {
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

func (bot *Bot) checkBan(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		userID := c.Sender().ID

		// Check if user exists and is banned
		user, err := bot.db.GetUser(userID)
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
	data := strings.Split(c.Data(), ":")
	action := strings.TrimSpace(data[0])

	handler, exists := bot.actionHandlers[action]
	if !exists {
		bot.log.Warnw("no handler for action", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}

	return handler.HandleAction(c, action)
}

func (bot *Bot) handleText(c tele.Context) error {
	userID := c.Sender().ID

	state, exists := bot.stateManager.GetState(userID)
	if !exists {
		return bot.suggestActions(c)
	}

	handler, exists := bot.stateHandlers[state]
	if !exists {
		bot.log.Warnw("no handler for state", "state", state)
		return bot.suggestActions(c)
	}

	return handler.HandleState(c, state)
}

func (bot *Bot) suggestActions(c tele.Context) error {
	inlineKeyboard := &tele.ReplyMarkup{}

	btnShowProfile := inlineKeyboard.Data("Показать мой профиль", btnShowProfile)
	btnChangeName := inlineKeyboard.Data("Изменить имя", btnChangeName)
	btnChangeBio := inlineKeyboard.Data("Изменить био", btnChangeBio)
	btnChangeTimezone := inlineKeyboard.Data("Изменить часовой пояс", btnChangeTimezone)
	btnChangePlans := inlineKeyboard.Data("Изменить планы на завтра", btnChangePlans)
	btnChangeWakeTime := inlineKeyboard.Data("Изменить время пробуждения", btnChangeWakeTime)
	btnChangeNotifyTime := inlineKeyboard.Data("Изменить время уведомления", btnChangeNotifyTime)
	btnInviteFriends := inlineKeyboard.Data("Пригласить друзей", "invite_friends")
	btnDoNothing := inlineKeyboard.Data("Ничего, до свидания", btnDoNothing)

	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnShowProfile),
		inlineKeyboard.Row(btnChangeName),
		inlineKeyboard.Row(btnChangeBio),
		inlineKeyboard.Row(btnChangeTimezone),
		inlineKeyboard.Row(btnChangePlans),
		inlineKeyboard.Row(btnChangeWakeTime),
		inlineKeyboard.Row(btnChangeNotifyTime),
		inlineKeyboard.Row(btnInviteFriends),
		inlineKeyboard.Row(btnDoNothing),
	)

	return c.Send("Похоже, вы не выполняете никаких действий. Что бы вы хотели сделать?", inlineKeyboard)
}

func (bot *Bot) handleStart(c tele.Context) error {
	state := StateRegistrationStart
	handler, exists := bot.stateHandlers[state]
	if !exists {
		bot.log.Warnw("no handler for state", "state", state)
		return bot.suggestActions(c)
	}

	return handler.HandleState(c, state)
}

func parseTime(timeStr string, userTz int32) (time.Time, error) {
	// Parse the time
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("неверный формат времени. Пожалуйста, используйте формат ЧЧ:ММ (например, 14:30)")
	}

	// Create a time.Location using the user's timezone offset
	userLoc := time.FixedZone("User Timezone", int(userTz)*60)

	// Set the time to today in the user's timezone
	now := time.Now().In(userLoc)
	userTime := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, userLoc)

	// If the resulting time is in the past, assume it's for tomorrow
	if userTime.Before(now) {
		userTime = userTime.Add(24 * time.Hour)
	}

	// Convert to UTC
	return userTime.UTC(), nil
}
