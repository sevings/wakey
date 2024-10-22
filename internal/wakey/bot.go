package wakey

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
)

type Bot struct {
	api          BotAPI
	db           *DB
	stateManager *StateManager
	planHandler  *PlanHandler
	wishHandler  *WishHandler
	adm          int64
	log          *zap.SugaredLogger
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

	return bot, true
}

func (bot *Bot) Start(cfg Config, api BotAPI) {
	bot.adm = cfg.AdminID
	bot.api = api
	bot.planHandler.api = api
	bot.wishHandler.api = api
	bot.wishHandler.adm = cfg.AdminID

	bot.api.Use(middleware.Recover())
	bot.api.Use(bot.logCmd)
	bot.api.Use(bot.checkBan)

	bot.api.Handle(tele.OnCallback, bot.handleCallback)
	bot.api.Handle("/start", bot.handleStart)
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
		return bot.handleBanCallback(c)
	case btnShowProfile:
		return bot.handleShowProfile(c)
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

func (bot *Bot) handleBanCallback(c tele.Context) error {
	if c.Sender().ID != bot.adm {
		return nil
	}

	data := strings.Split(c.Data(), ":")
	action := data[0]
	userIDStr := data[1]

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return c.Edit("Ошибка при обработке ID пользователя.")
	}

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to get user", "error", err, "userID", userID)
		return c.Edit("Ошибка при получении информации о пользователе.")
	}

	switch action {
	case btnBanUser:
		user.IsBanned = true
		if err := bot.db.SaveUser(user); err != nil {
			bot.log.Errorw("failed to ban user", "error", err, "userID", userID)
			return c.Edit("Ошибка при бане пользователя.")
		}

		// Notify the banned user
		banMessage := "Вы были забанены за нарушение правил использования бота. Вы больше не сможете отправлять или получать пожелания."
		_, err = bot.api.Send(tele.ChatID(userID), banMessage)
		if err != nil {
			bot.log.Errorw("failed to send ban notification to user", "error", err, "userID", userID)
		}

		return c.Edit(fmt.Sprintf("Пользователь %d забанен и уведомлен.", userID))
	case btnSkipBan:
		return c.Edit(fmt.Sprintf("Бан пользователя %d пропущен.", userID))
	default:
		return c.Edit("Неизвестное действие.")
	}
}

func (bot *Bot) handleStart(c tele.Context) error {
	userID := c.Sender().ID

	// Check if user already exists
	user, err := bot.db.GetUser(userID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		bot.log.Errorw("failed to check user existence", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}
	if err != ErrNotFound {
		welcomeBack := fmt.Sprintf("С возвращением, %s! Вы уже зарегистрированы.", user.Name)
		fullMessage := welcomeBack + "\n\n" + bot.getWelcomeMessage()
		return c.Send(fullMessage)
	}

	// Start registration process
	bot.stateManager.SetState(userID, StateAwaitingName)
	welcomeMessage := "Добро пожаловать! Давайте зарегистрируем вас. Но сначала, позвольте рассказать о моих возможностях.\n\n"
	welcomeMessage += bot.getWelcomeMessage()
	welcomeMessage += "\n\nТеперь давайте начнем регистрацию. Как вас зовут?"
	return c.Send(welcomeMessage)
}

func (bot *Bot) getWelcomeMessage() string {
	return `Я бот, который поможет вам планировать ваш день и обмениваться пожеланиями с другими пользователями. Вот что я умею:

1. Сохранять ваши ежедневные планы и время пробуждения.
2. Напоминать вам о необходимости обновить планы каждый вечер.
3. Позволять вам отправлять пожелания другим пользователям.
4. Доставлять пожелания от других пользователей в момент вашего пробуждения.

Вот несколько команд, которые вам пригодятся:
• /set_plans - обновить ваши планы и время пробуждения
• /show_plan - показать ваш текущий план

Надеюсь, мы отлично проведем время вместе!`
}

func (bot *Bot) handleText(c tele.Context) error {
	userID := c.Sender().ID

	state, exists := bot.stateManager.GetState(userID)
	if !exists {
		return bot.suggestActions(c)
	}

	switch state {
	case StateAwaitingName:
		return bot.handleNameInput(c)
	case StateAwaitingBio:
		return bot.handleBioInput(c)
	case StateAwaitingTime:
		return bot.handleTimeInput(c)
	case StateAwaitingPlans:
		return bot.planHandler.HandlePlansInput(c)
	case StateAwaitingWakeTime:
		return bot.planHandler.HandleWakeTimeInput(c)
	case StateAwaitingWish:
		return bot.wishHandler.HandleWishInput(c)
	case StateAwaitingNotificationTime:
		return bot.planHandler.HandleNotificationTimeInput(c)
	case StateUpdatingName:
		return bot.handleNameUpdate(c)
	case StateUpdatingBio:
		return bot.handleBioUpdate(c)
	case StateUpdatingTimezone:
		return bot.handleTimezoneUpdate(c)
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

func (bot *Bot) handleNameInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := bot.stateManager.GetUserData(userID)
	userData.Name = c.Text()
	bot.stateManager.SetUserData(userID, userData)
	bot.stateManager.SetState(userID, StateAwaitingBio)
	return c.Send("Приятно познакомиться, " + userData.Name + "! Теперь, пожалуйста, расскажите немного о себе (краткое био).")
}

func (bot *Bot) handleBioInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := bot.stateManager.GetUserData(userID)
	userData.Bio = c.Text()
	bot.stateManager.SetUserData(userID, userData)
	bot.stateManager.SetState(userID, StateAwaitingTime)
	return c.Send("Отлично! Наконец, скажите, который сейчас у вас час? (Пожалуйста, используйте формат ЧЧ:ММ)")
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

func (bot *Bot) handleTimeInput(c tele.Context) error {
	userID := c.Sender().ID
	timeStr := c.Text()

	userTime, err := parseTime(timeStr, 0) // Use 0 as initial timezone offset
	if err != nil {
		return c.Send(err.Error())
	}

	// Calculate timezone offset
	tzOffset := int32(userTime.Sub(time.Now().UTC()).Minutes())

	userData, _ := bot.stateManager.GetUserData(userID)

	// Save user to database
	user := User{
		ID:   userID,
		Name: userData.Name,
		Bio:  userData.Bio,
		Tz:   tzOffset,
	}
	if err := bot.db.SaveUser(&user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Registration complete
	bot.stateManager.SetState(userID, StateAwaitingPlans)
	return c.Send("Отлично! Теперь расскажите о ваших планах на завтра.")
}

func (bot *Bot) handleShowProfile(c tele.Context) error {
	userID := c.Sender().ID

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Edit("Извините, произошла ошибка при загрузке вашего профиля. Пожалуйста, попробуйте позже.")
	}

	plan, err := bot.db.GetLatestPlan(userID)
	if err != nil && err != ErrNotFound {
		bot.log.Errorw("failed to load latest plan", "error", err)
		return c.Edit("Извините, произошла ошибка при загрузке ваших планов. Пожалуйста, попробуйте позже.")
	}

	userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)
	localWakeTime := "Не установлено"
	localNotifyTime := user.NotifyAt.In(userLoc).Format("15:04")

	if plan != nil {
		localWakeTime = plan.WakeAt.In(userLoc).Format("15:04")
	}

	profileMsg := fmt.Sprintf("Ваш профиль:\n\n"+
		"Имя: %s\n"+
		"Био: %s\n"+
		"Часовой пояс: UTC%+d\n"+
		"Время пробуждения: %s\n"+
		"Время уведомления: %s\n",
		user.Name, user.Bio, user.Tz/60, localWakeTime, localNotifyTime)

	if plan != nil {
		profileMsg += fmt.Sprintf("Текущие планы: %s", plan.Content)
	} else {
		profileMsg += "Текущие планы: Не установлены"
	}

	// First, edit the current message to show the profile
	err = c.Edit(profileMsg)
	if err != nil {
		bot.log.Errorw("failed to edit message with profile", "error", err)
		return err
	}

	// Then, send a new message with suggested actions
	return bot.suggestActions(c)
}

func (bot *Bot) handleNameUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newName := c.Text()

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Name = newName
	if err := bot.db.SaveUser(user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	bot.stateManager.ClearState(userID)
	return c.Send(fmt.Sprintf("Ваше имя успешно обновлено на %s.", newName))
}

func (bot *Bot) handleBioUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newBio := c.Text()

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Bio = newBio
	if err := bot.db.SaveUser(user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	bot.stateManager.ClearState(userID)
	return c.Send("Ваше био успешно обновлено.")
}

func (bot *Bot) handleTimezoneUpdate(c tele.Context) error {
	userID := c.Sender().ID
	timeStr := c.Text()

	userTime, err := parseTime(timeStr, 0) // Use 0 as initial timezone offset
	if err != nil {
		return c.Send(err.Error())
	}
	tzOffset := int32(userTime.Sub(time.Now().UTC()).Minutes())

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Tz = tzOffset
	if err := bot.db.SaveUser(user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	bot.stateManager.ClearState(userID)
	return c.Send("Ваш часовой пояс успешно обновлен.")
}
