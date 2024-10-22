package wakey

import (
	"fmt"
	"strconv"
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
	planHandler    *PlanHandler
	wishHandler    *WishHandler
	profileHandler *ProfileHandler
	adminHandler   *AdminHandler
	adm            int64
	log            *zap.SugaredLogger
}

type BotAPI interface {
	Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error)
	Handle(endpoint interface{}, h tele.HandlerFunc, m ...tele.MiddlewareFunc)
	Use(middlewares ...tele.MiddlewareFunc)
	Start()
	Stop()
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
	btnDoNothing        = "do_nothing"
	btnBanUser          = "ban_user"
	btnSkipBan          = "skip_ban"
)

func NewBot(db *DB, wishSched, planSched Scheduler) (*Bot, bool) {
	bot := &Bot{
		db:           db,
		stateManager: NewStateManager(),
		log:          zap.L().Named("bot").Sugar(),
	}

	bot.planHandler = NewPlanHandler(db, planSched, bot.stateManager, bot.log)
	bot.wishHandler = NewWishHandler(db, wishSched, bot.stateManager, bot.log)
	bot.profileHandler = NewProfileHandler(db, bot.stateManager, bot.log)
	bot.adminHandler = NewAdminHandler(db, bot.log)

	return bot, true
}

func (bot *Bot) Start(cfg Config, api BotAPI) {
	bot.adm = cfg.AdminID
	bot.api = api
	bot.planHandler.api = api
	bot.wishHandler.api = api
	bot.wishHandler.adm = cfg.AdminID
	bot.adminHandler.api = api
	bot.adminHandler.adm = cfg.AdminID

	bot.api.Use(middleware.Recover())
	bot.api.Use(bot.logCmd)
	bot.api.Use(bot.checkBan)

	bot.api.Handle(tele.OnCallback, bot.handleCallback)
	bot.api.Handle("/start", bot.profileHandler.HandleStart)
	bot.api.Handle("/set_plans", bot.planHandler.HandleSetPlans)
	bot.api.Handle("/show_plan", bot.planHandler.HandleShowPlan)
	bot.api.Handle(tele.OnText, bot.handleText)

	// Load and schedule future wishes
	bot.wishHandler.ScheduleFutureWishes()

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
	return func(c tele.Context) error {
		beginTime := time.Now().UnixNano()
		isBotCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1

		err := next(c)

		if isBotCmd {
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

	switch action {
	case btnSendWishYes:
		return bot.wishHandler.HandleSendWishResponse(c)
	case btnSendWishNo:
		return bot.wishHandler.HandleSendWishNo(c)
	case btnWishDislike:
		return bot.wishHandler.HandleWishDislike(c)
	case btnKeepPlans, btnUpdatePlans, btnNoWish:
		return bot.planHandler.HandlePlanReminderCallback(c)
	case btnBanUser, btnSkipBan:
		return bot.adminHandler.HandleBanCallback(c)
	case btnShowProfile:
		return bot.profileHandler.HandleShowProfile(c)
	case btnChangeName, btnChangeBio, btnChangeTimezone, btnChangePlans, btnChangeWakeTime, btnChangeNotifyTime, btnDoNothing:
		return bot.handleActionCallback(c)
	}

	// For actions that require an ID
	if len(data) != 2 {
		return c.Send("Неверный формат данных.")
	}

	wishID, err := strconv.ParseUint(data[1], 10, 64)
	if err != nil {
		return c.Send("Неверный ID пожелания.")
	}

	wish, err := bot.db.GetWishByID(uint(wishID))
	if err != nil {
		return c.Send("Не удалось найти пожелание.")
	}

	switch action {
	case btnWishLike:
		return bot.wishHandler.HandleWishLike(c, wish)
	case btnWishReport:
		return bot.wishHandler.HandleWishReport(c, wish)
	default:
		return c.Send("Неизвестный выбор.")
	}
}

func (bot *Bot) handleActionCallback(c tele.Context) error {
	action := c.Data()
	userID := c.Sender().ID

	switch action {
	case btnChangeName:
		bot.stateManager.SetState(userID, StateUpdatingName)
		return c.Edit("Пожалуйста, введите ваше новое имя.")
	case btnChangeBio:
		bot.stateManager.SetState(userID, StateUpdatingBio)
		return c.Edit("Пожалуйста, введите ваше новое био.")
	case btnChangeTimezone:
		bot.stateManager.SetState(userID, StateUpdatingTimezone)
		return c.Edit("Пожалуйста, введите текущее время в формате ЧЧ:ММ.")
	case btnChangePlans:
		bot.stateManager.SetState(userID, StateUpdatingPlans)
		return c.Edit("Пожалуйста, введите ваши новые планы на завтра.")
	case btnChangeWakeTime:
		bot.stateManager.SetState(userID, StateUpdatingWakeTime)
		return c.Edit("Пожалуйста, введите новое время пробуждения в формате ЧЧ:ММ.")
	case btnChangeNotifyTime:
		bot.stateManager.SetState(userID, StateUpdatingNotificationTime)
		return c.Edit("Пожалуйста, введите новое время уведомления в формате ЧЧ:ММ.")
	case btnDoNothing:
		bot.stateManager.ClearState(userID)
		return c.Edit("Хорошо, до свидания! Если вам что-то понадобится, просто напишите мне.")
	default:
		return c.Edit("Неизвестный выбор. Пожалуйста, попробуйте еще раз.")
	}
}

func (bot *Bot) handleText(c tele.Context) error {
	userID := c.Sender().ID

	state, exists := bot.stateManager.GetState(userID)
	if !exists {
		return bot.suggestActions(c)
	}

	switch state {
	case StateAwaitingName:
		return bot.profileHandler.HandleNameInput(c)
	case StateAwaitingBio:
		return bot.profileHandler.HandleBioInput(c)
	case StateAwaitingTime:
		return bot.profileHandler.HandleTimeInput(c)
	case StateAwaitingPlans:
		return bot.planHandler.HandlePlansInput(c)
	case StateAwaitingWakeTime:
		return bot.planHandler.HandleWakeTimeInput(c)
	case StateAwaitingWish:
		return bot.wishHandler.HandleWishInput(c)
	case StateAwaitingNotificationTime:
		return bot.planHandler.HandleNotificationTimeInput(c)
	case StateUpdatingName:
		return bot.profileHandler.HandleNameUpdate(c)
	case StateUpdatingBio:
		return bot.profileHandler.HandleBioUpdate(c)
	case StateUpdatingTimezone:
		return bot.profileHandler.HandleTimezoneUpdate(c)
	case StateUpdatingPlans:
		return bot.planHandler.HandlePlansUpdate(c)
	case StateUpdatingWakeTime:
		return bot.planHandler.HandleWakeTimeUpdate(c)
	case StateUpdatingNotificationTime:
		return bot.planHandler.HandleNotificationTimeUpdate(c)
	default:
		return nil
	}
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
	btnDoNothing := inlineKeyboard.Data("Ничего, до свидания", btnDoNothing)

	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnShowProfile),
		inlineKeyboard.Row(btnChangeName),
		inlineKeyboard.Row(btnChangeBio),
		inlineKeyboard.Row(btnChangeTimezone),
		inlineKeyboard.Row(btnChangePlans),
		inlineKeyboard.Row(btnChangeWakeTime),
		inlineKeyboard.Row(btnChangeNotifyTime),
		inlineKeyboard.Row(btnDoNothing),
	)

	return c.Send("Похоже, вы не выполняете никаких действий. Что бы вы хотели сделать?", inlineKeyboard)
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
