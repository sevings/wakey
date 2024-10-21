package wakey

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
)

type Bot struct {
	bot        *tele.Bot
	db         *DB
	wishSched  Scheduler
	planSched  Scheduler
	userStates map[int64]*UserData
	stateMutex sync.Mutex
	adm        int64
	log        *zap.SugaredLogger
}

type JobID uint
type JobFunc func(JobID)

type Scheduler interface {
	SetJobFunc(fn JobFunc)
	Schedule(at time.Time, id JobID)
	Cancel(id JobID)
}

type UserState int

const (
	StateNone UserState = iota
	StateAwaitingName
	StateAwaitingBio
	StateAwaitingTime
	StateAwaitingPlans
	StateAwaitingWakeTime
	StateAwaitingWish
	StateAwaitingNotificationTime
	StateUpdatingName
	StateUpdatingBio
	StateUpdatingTimezone
	StateUpdatingPlans
	StateUpdatingWakeTime
	StateUpdatingNotificationTime
)

const (
	btnWishLike         = "wish_like"
	btnWishDislike      = "wish_dislike"
	btnWishReport       = "wish_report"
	btnSendWishYes      = "send_wish_yes"
	btnSendWishNo       = "send_wish_no"
	btnKeepPlans        = "keep_plans"
	btnUpdatePlans      = "update_plans"
	btnNoWish           = "no_wish"
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

type UserData struct {
	State          UserState
	IsSingleAction bool
	Name           string
	Bio            string
	Plans          string
	TargetUserID   int64
	TargetPlanID   uint
}

func NewBot(cfg Config, db *DB, wishSched, planSched Scheduler) (*Bot, bool) {
	bot := &Bot{
		db:         db,
		wishSched:  wishSched,
		planSched:  planSched,
		userStates: make(map[int64]*UserData),
		adm:        cfg.AdminID,
		log:        zap.L().Named("bot").Sugar(),
	}

	bot.wishSched.SetJobFunc(bot.SendWish)
	bot.planSched.SetJobFunc(bot.AskAboutPlans)

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

	bot.bot.Handle(tele.OnCallback, bot.handleCallback)
	bot.bot.Handle("/start", bot.handleStart)
	bot.bot.Handle("/set_plans", bot.handleSetPlans)
	bot.bot.Handle("/show_plan", bot.handleShowPlan)
	bot.bot.Handle(tele.OnText, bot.handleText)

	// Load and schedule future wishes
	bot.scheduleFutureWishes()

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

func (bot *Bot) handleCallback(c tele.Context) error {
	userID := c.Sender().ID

	// Check if user exists and is banned
	user, err := bot.db.GetUser(userID)
	if err == nil && user.IsBanned {
		return c.Send(&tele.CallbackResponse{
			Text:      "Извините, вы не можете использовать бота, так как были забанены.",
			ShowAlert: true,
		})
	}

	data := strings.Split(c.Data(), ":")
	action := strings.TrimSpace(data[0])

	switch action {
	case btnSendWishYes:
		return bot.handleSendWishResponse(c)
	case btnSendWishNo:
		return bot.handleSendWishNo(c)
	case btnWishDislike:
		return bot.handleWishDislike(c)
	case btnKeepPlans, btnUpdatePlans, btnNoWish:
		return bot.handlePlanReminderCallback(c)
	case btnBanUser, btnSkipBan:
		return bot.handleBanCallback(c)
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
		return bot.handleWishLike(c, wish)
	case btnWishReport:
		return bot.handleWishReport(c, wish)
	default:
		return c.Send("Неизвестный выбор.")
	}
}

func (bot *Bot) handleActionCallback(c tele.Context) error {
	action := c.Data()
	userID := c.Sender().ID

	bot.stateMutex.Lock()
	defer bot.stateMutex.Unlock()

	switch action {
	case btnChangeName:
		bot.userStates[userID] = &UserData{State: StateUpdatingName}
		return c.Edit("Пожалуйста, введите ваше новое имя.")
	case btnChangeBio:
		bot.userStates[userID] = &UserData{State: StateUpdatingBio}
		return c.Edit("Пожалуйста, введите ваше новое био.")
	case btnChangeTimezone:
		bot.userStates[userID] = &UserData{State: StateUpdatingTimezone}
		return c.Edit("Пожалуйста, введите текущее время в формате ЧЧ:ММ.")
	case btnChangePlans:
		bot.userStates[userID] = &UserData{State: StateUpdatingPlans}
		return c.Edit("Пожалуйста, введите ваши новые планы на завтра.")
	case btnChangeWakeTime:
		bot.userStates[userID] = &UserData{State: StateUpdatingWakeTime}
		return c.Edit("Пожалуйста, введите новое время пробуждения в формате ЧЧ:ММ.")
	case btnChangeNotifyTime:
		bot.userStates[userID] = &UserData{State: StateUpdatingNotificationTime}
		return c.Edit("Пожалуйста, введите новое время уведомления в формате ЧЧ:ММ.")
	case btnDoNothing:
		delete(bot.userStates, userID)
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
		_, err = bot.bot.Send(tele.ChatID(userID), banMessage)
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

	// Check if user already exists and is banned
	user, err := bot.db.GetUser(userID)
	if err == nil && user.IsBanned {
		return c.Send("Извините, вы не можете использовать бота, так как были забанены.")
	}

	// Check if user already exists
	if err != nil && !errors.Is(err, ErrNotFound) {
		bot.log.Errorw("failed to check user existence", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}
	if err != ErrNotFound {
		welcomeBack := fmt.Sprintf("С возвращением, %s! Вы уже зарегистрированы.", user.Name)
		fullMessage := welcomeBack + "\n\n" + bot.getWelcomeMessage()
		return c.Send(fullMessage)
	}

	bot.stateMutex.Lock()
	defer bot.stateMutex.Unlock()

	// Start registration process
	bot.userStates[userID] = &UserData{State: StateAwaitingName}
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

func (bot *Bot) handleSetPlans(c tele.Context) error {
	userID := c.Sender().ID

	// Check if user exists and is banned
	user, err := bot.db.GetUser(userID)
	if err == nil && user.IsBanned {
		return c.Send("Извините, вы не можете использовать бота, так как были забанены.")
	}

	return bot.handlePlansAndWakeTime(c)
}

func (bot *Bot) handleText(c tele.Context) error {
	userID := c.Sender().ID

	// Check if the user exists and is banned
	user, err := bot.db.GetUser(userID)
	if err == nil && user.IsBanned {
		return c.Send("Извините, вы не можете использовать бота, так как были забанены.")
	}

	bot.stateMutex.Lock()
	defer bot.stateMutex.Unlock()

	state, exists := bot.userStates[userID]
	if !exists {
		return bot.suggestActions(c)
	}

	switch state.State {
	case StateAwaitingName:
		return bot.handleNameInput(c, state)
	case StateAwaitingBio:
		return bot.handleBioInput(c, state)
	case StateAwaitingTime:
		return bot.handleTimeInput(c, state)
	case StateAwaitingPlans:
		return bot.handlePlansInput(c, state)
	case StateAwaitingWakeTime:
		return bot.handleWakeTimeInput(c, state)
	case StateAwaitingWish:
		return bot.handleWishInput(c, state)
	case StateAwaitingNotificationTime:
		return bot.handleNotificationTimeInput(c)
	case StateUpdatingName:
		return bot.handleNameUpdate(c)
	case StateUpdatingBio:
		return bot.handleBioUpdate(c)
	case StateUpdatingTimezone:
		return bot.handleTimezoneUpdate(c)
	case StateUpdatingPlans:
		return bot.handlePlansUpdate(c)
	case StateUpdatingWakeTime:
		return bot.handleWakeTimeUpdate(c)
	case StateUpdatingNotificationTime:
		return bot.handleNotificationTimeUpdate(c)
	default:
		return nil
	}
}

func (bot *Bot) suggestActions(c tele.Context) error {
	inlineKeyboard := &tele.ReplyMarkup{}

	btnChangeName := inlineKeyboard.Data("Изменить имя", btnChangeName)
	btnChangeBio := inlineKeyboard.Data("Изменить био", btnChangeBio)
	btnChangeTimezone := inlineKeyboard.Data("Изменить часовой пояс", btnChangeTimezone)
	btnChangePlans := inlineKeyboard.Data("Изменить планы на завтра", btnChangePlans)
	btnChangeWakeTime := inlineKeyboard.Data("Изменить время пробуждения", btnChangeWakeTime)
	btnChangeNotifyTime := inlineKeyboard.Data("Изменить время уведомления", btnChangeNotifyTime)
	btnDoNothing := inlineKeyboard.Data("Ничего, до свидания", btnDoNothing)

	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnChangeName, btnChangeBio),
		inlineKeyboard.Row(btnChangeTimezone, btnChangePlans),
		inlineKeyboard.Row(btnChangeWakeTime, btnChangeNotifyTime),
		inlineKeyboard.Row(btnDoNothing),
	)

	return c.Send("Похоже, вы не выполняете никаких действий. Что бы вы хотели сделать?", inlineKeyboard)
}

func (bot *Bot) handleNameInput(c tele.Context, state *UserData) error {
	state.Name = c.Text()
	state.State = StateAwaitingBio
	return c.Send("Приятно познакомиться, " + state.Name + "! Теперь, пожалуйста, расскажите немного о себе (краткое био).")
}

func (bot *Bot) handleBioInput(c tele.Context, state *UserData) error {
	state.Bio = c.Text()
	state.State = StateAwaitingTime
	return c.Send("Отлично! Наконец, скажите, который сейчас у вас час? (Пожалуйста, используйте формат ЧЧ:ММ)")
}

func (bot *Bot) handleTimeInput(c tele.Context, state *UserData) error {
	timeStr := c.Text()

	// Parse the time
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return c.Send("Извините, я не смог понять это время. Пожалуйста, попробуйте еще раз, используя формат ЧЧ:ММ (например, 14:30)")
	}

	// Calculate timezone offset
	now := time.Now().UTC()
	userTime := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	tzOffset := int32(userTime.Sub(now).Minutes())

	// Save user to database
	user := User{
		ID:   c.Sender().ID,
		Name: state.Name,
		Bio:  state.Bio,
		Tz:   tzOffset,
	}
	if err := bot.db.SaveUser(&user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Registration complete
	state.State = StateAwaitingPlans
	return c.Send("Отлично! Теперь расскажите о ваших планах на завтра.")
}

func (bot *Bot) handlePlansAndWakeTime(c tele.Context) error {
	userID := c.Sender().ID

	bot.stateMutex.Lock()
	defer bot.stateMutex.Unlock()

	state, exists := bot.userStates[userID]
	if !exists {
		state = &UserData{State: StateAwaitingPlans}
		bot.userStates[userID] = state
	}

	switch state.State {
	case StateAwaitingPlans:
		return bot.handlePlansInput(c, state)
	case StateAwaitingWakeTime:
		return bot.handleWakeTimeInput(c, state)
	default:
		return c.Send("Что-то пошло не так. Пожалуйста, попробуйте еще раз.")
	}
}

func (bot *Bot) handlePlansInput(c tele.Context, state *UserData) error {
	state.Plans = c.Text()
	state.State = StateAwaitingWakeTime
	return c.Send("Отлично! Теперь скажите, во сколько вы планируете проснуться завтра? (Используйте формат ЧЧ:ММ)")
}

func (bot *Bot) handleWakeTimeInput(c tele.Context, state *UserData) error {
	wakeTimeStr := c.Text()

	// Load user to get timezone
	user, err := bot.db.GetUser(c.Sender().ID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Parse the time
	localWakeTime, err := time.Parse("15:04", wakeTimeStr)
	if err != nil {
		return c.Send("Извините, я не смог понять это время. Пожалуйста, попробуйте еще раз, используя формат ЧЧ:ММ (например, 07:30)")
	}

	// Create a time.Location using the user's timezone offset
	userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)

	// Set the wake time to tomorrow in the user's timezone
	now := time.Now().In(userLoc)
	localWakeTime = time.Date(now.Year(), now.Month(), now.Day()+1, localWakeTime.Hour(), localWakeTime.Minute(), 0, 0, userLoc)

	// Convert local wake time to UTC
	utcWakeTime := localWakeTime.UTC()

	// Save plan to database
	plan := &Plan{
		UserID:  c.Sender().ID,
		Content: state.Plans,
		WakeAt:  utcWakeTime,
	}

	if err := bot.db.SavePlan(plan); err != nil {
		bot.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	delete(bot.userStates, c.Sender().ID)

	err = c.Send("Спасибо! Ваши планы и время пробуждения сохранены.")
	if err != nil {
		return err
	}

	// Ask if the user wants to send a wish
	inlineKeyboard := &tele.ReplyMarkup{}
	btnYes := inlineKeyboard.Data("Да", btnSendWishYes)
	btnNo := inlineKeyboard.Data("Нет", btnSendWishNo)
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnYes, btnNo),
	)

	return c.Send("Хотите отправить пожелание другому пользователю?", inlineKeyboard)
}

func (bot *Bot) handleWishLike(c tele.Context, wish *Wish) error {
	// Send message to the wish author
	thanksMsg := fmt.Sprintf("Пользователю %s понравилось ваше пожелание.", wish.Plan.User.Name)
	_, err := bot.bot.Send(tele.ChatID(wish.FromID), thanksMsg)
	if err != nil {
		bot.log.Errorw("failed to send thanks message", "error", err, "userID", wish.FromID)
	}

	// Remove the inline keyboard
	return bot.removeWishKeyboard(c)
}

func (bot *Bot) handleWishDislike(c tele.Context) error {
	// Just remove the inline keyboard
	return bot.removeWishKeyboard(c)
}

func (bot *Bot) handleWishReport(c tele.Context, wish *Wish) error {
	if bot.adm != 0 {
		reportMsg := fmt.Sprintf("Жалоба на пожелание:\n\nАвтор ID: %d\nТекст пожелания: %s", wish.FromID, wish.Content)

		inlineKeyboard := &tele.ReplyMarkup{}
		btnBan := inlineKeyboard.Data("Забанить", btnBanUser, fmt.Sprintf("%d", wish.FromID))
		btnSkip := inlineKeyboard.Data("Пропустить", btnSkipBan, fmt.Sprintf("%d", wish.FromID))
		inlineKeyboard.Inline(
			inlineKeyboard.Row(btnBan, btnSkip),
		)

		_, err := bot.bot.Send(tele.ChatID(bot.adm), reportMsg, inlineKeyboard)
		if err != nil {
			bot.log.Errorw("failed to send report to admin", "error", err)
		}
	}

	return bot.removeWishKeyboard(c)
}

func (bot *Bot) removeWishKeyboard(c tele.Context) error {
	// Edit the original message to remove the inline keyboard
	err := c.Edit(c.Message().Text)
	if err != nil {
		bot.log.Errorw("failed to remove wish keyboard", "error", err)
		return c.Send("Произошла ошибка при обработке вашего ответа.")
	}

	return c.Send("Спасибо за ваш ответ!")
}

func (bot *Bot) handleSendWishResponse(c tele.Context) error {
	// Remove the inline keyboard
	err := c.Edit("Хорошо, давайте отправим пожелание!")
	if err != nil {
		bot.log.Errorw("failed to remove send wish keyboard", "error", err)
	}

	return bot.findUserForWish(c)
}

func (bot *Bot) handleSendWishNo(c tele.Context) error {
	// Remove the inline keyboard
	err := c.Edit("Хорошо, может быть в следующий раз!")
	if err != nil {
		bot.log.Errorw("failed to remove send wish keyboard", "error", err)
		return c.Send("Произошла ошибка при обработке вашего ответа.")
	}
	return nil
}

func (bot *Bot) findUserForWish(c tele.Context) error {
	senderID := c.Sender().ID

	// Check if sender is banned
	sender, err := bot.db.GetUser(senderID)
	if err == nil && sender.IsBanned {
		return c.Send("Извините, вы не можете отправлять пожелания, так как были забанены.")
	}

	plan, err := bot.db.FindUserForWish(senderID)
	if err != nil {
		if err == ErrNotFound {
			return c.Send("К сожалению, сейчас нет пользователей, которым можно отправить пожелание.")
		}
		bot.log.Errorw("failed to find user for wish", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	userInfo := fmt.Sprintf("Имя: %s\nО себе: %s\nПланы: %s",
		plan.User.Name, plan.User.Bio, plan.Content)
	bot.userStates[c.Sender().ID] = &UserData{
		State:        StateAwaitingWish,
		TargetUserID: plan.User.ID,
		TargetPlanID: plan.ID,
	}

	return c.Send(fmt.Sprintf("Отправьте ваше пожелание для этого пользователя:\n\n%s", userInfo))
}

func utcToUserLocal(utcTime time.Time, tzOffset int32) time.Time {
	userLoc := time.FixedZone("User Timezone", int(tzOffset)*60)
	return utcTime.In(userLoc)
}

func (bot *Bot) handleShowPlan(c tele.Context) error {
	userID := c.Sender().ID

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	if user.IsBanned {
		return c.Send("Извините, вы не можете использовать бота, так как были забанены.")
	}

	plan, err := bot.db.GetLatestPlan(c.Sender().ID)
	if err != nil {
		if err == ErrNotFound {
			return c.Send("У вас пока нет сохраненных планов.")
		}
		bot.log.Errorw("failed to get latest plan", "error", err)
		return c.Send("Извините, произошла ошибка при получении ваших планов.")
	}

	localWakeTime := utcToUserLocal(plan.WakeAt, user.Tz)

	message := fmt.Sprintf("Ваши текущие планы:\n\nПлан: %s\nВремя пробуждения: %s",
		plan.Content, localWakeTime.Format("15:04"))

	return c.Send(message)
}

func (bot *Bot) handleWishInput(c tele.Context, state *UserData) error {
	plan, err := bot.db.GetPlanByID(state.TargetPlanID)
	if err != nil {
		bot.log.Errorw("failed to get plan", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	if time.Now().UTC().Sub(plan.OfferedAt) > time.Hour {
		delete(bot.userStates, c.Sender().ID)
		return c.Send("Извините, время для отправки пожелания истекло. Пожалуйста, попробуйте отправить новое пожелание.")
	}

	wish := &Wish{
		FromID:  c.Sender().ID,
		PlanID:  state.TargetPlanID,
		Content: c.Text(),
	}

	if err := bot.db.SaveWish(wish); err != nil {
		bot.log.Errorw("failed to save wish", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашего пожелания. Пожалуйста, попробуйте позже.")
	}

	bot.wishSched.Schedule(plan.WakeAt, JobID(wish.ID))

	delete(bot.userStates, c.Sender().ID)
	return c.Send("Спасибо! Ваше пожелание отправлено и будет доставлено пользователю в запланированное время.")
}

func (bot *Bot) SendWish(id JobID) {
	wishID := uint(id)
	wish, err := bot.db.GetWishByID(wishID)
	if err != nil {
		bot.log.Errorw("failed to get wish", "error", err, "wishID", wishID)
		return
	}

	// Check if the recipient is banned
	recipient, err := bot.db.GetUser(wish.Plan.UserID)
	if err != nil {
		bot.log.Errorw("failed to get recipient", "error", err, "userID", wish.Plan.UserID)
		return
	}

	if recipient.IsBanned {
		bot.log.Infow("skipping wish for banned user", "userID", wish.Plan.UserID)
		return
	}

	message := fmt.Sprintf("Доброе утро! Вот пожелание для вас:\n\n%s", wish.Content)

	// Create inline keyboard
	inlineKeyboard := &tele.ReplyMarkup{}
	btnLike := inlineKeyboard.Data("Спасибо, понравилось", btnWishLike, fmt.Sprintf("%d", wishID))
	btnDislike := inlineKeyboard.Data("Ну такое…", btnWishDislike, fmt.Sprintf("%d", wishID))
	btnReport := inlineKeyboard.Data("Пожаловаться", btnWishReport, fmt.Sprintf("%d", wishID))
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnLike),
		inlineKeyboard.Row(btnDislike),
		inlineKeyboard.Row(btnReport),
	)

	// Send message with inline keyboard
	_, err = bot.bot.Send(tele.ChatID(wish.Plan.UserID), message, inlineKeyboard)
	if err != nil {
		bot.log.Errorw("failed to send wish", "error", err, "userID", wish.Plan.UserID)
	}
}

func (bot *Bot) scheduleFutureWishes() {
	wishes, err := bot.db.GetFutureWishes()
	if err != nil {
		bot.log.Errorw("failed to schedule future wishes", "error", err)
		return
	}

	for _, wish := range wishes {
		bot.wishSched.Schedule(wish.Plan.WakeAt, JobID(wish.ID))
		bot.log.Infow("scheduled wish", "wishID", wish.ID, "wakeAt", wish.Plan.WakeAt)
	}

	bot.log.Infof("Scheduled %d future wishes", len(wishes))
}

// Update handleNotificationTimeInput function
func (bot *Bot) handleNotificationTimeInput(c tele.Context) error {
	notificationTimeStr := c.Text()

	// Parse the time
	notificationTime, err := time.Parse("15:04", notificationTimeStr)
	if err != nil {
		return c.Send("Извините, я не смог понять это время. Пожалуйста, попробуйте еще раз, используя формат ЧЧ:ММ (например, 21:00)")
	}

	userID := c.Sender().ID
	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err, "userID", userID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Convert to UTC
	now := time.Now().UTC()
	userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)
	notifyAtUTC := time.Date(now.Year(), now.Month(), now.Day(), notificationTime.Hour(), notificationTime.Minute(), 0, 0, userLoc).UTC()

	user.NotifyAt = notifyAtUTC
	// Save user to database

	if err := bot.db.SaveUser(user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Schedule asking about plans
	bot.schedulePlanReminder(user)

	// Registration complete
	delete(bot.userStates, c.Sender().ID)
	return c.Send(fmt.Sprintf("Отлично! Регистрация завершена. Я буду напоминать вам о планах каждый день в %s.", notificationTimeStr))
}

// Update schedulePlanReminder function
func (bot *Bot) schedulePlanReminder(user *User) {
	now := time.Now().UTC()
	nextNotification := user.NotifyAt

	if nextNotification.Before(now) {
		nextNotification = nextNotification.Add(24 * time.Hour)
	}

	bot.planSched.Schedule(nextNotification, JobID(user.ID))
}

// Update AskAboutPlans function
func (bot *Bot) AskAboutPlans(id JobID) {
	userID := int64(id)
	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err, "userID", userID)
		return
	}

	// Create inline keyboard
	inlineKeyboard := &tele.ReplyMarkup{}
	btnKeep := inlineKeyboard.Data("Оставить как есть", btnKeepPlans)
	btnUpdate := inlineKeyboard.Data("Обновить планы", btnUpdatePlans)
	btnNoWish := inlineKeyboard.Data("Не получать пожелание", btnNoWish)
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnKeep),
		inlineKeyboard.Row(btnUpdate),
		inlineKeyboard.Row(btnNoWish),
	)

	_, err = bot.bot.Send(tele.ChatID(userID), "Пора рассказать о ваших планах на завтра! Что вы хотите сделать?", inlineKeyboard)
	if err != nil {
		bot.log.Errorw("failed to send plan reminder", "error", err, "userID", userID)
	}

	// Reschedule for the next day
	bot.schedulePlanReminder(user)
}

// Add new handler for plan reminder callback
func (bot *Bot) handlePlanReminderCallback(c tele.Context) error {
	action := c.Data()
	userID := c.Sender().ID

	switch action {
	case btnKeepPlans:
		// Keep plans and wake up time the same
		err := bot.db.CopyPlanForNextDay(userID)
		if err != nil {
			bot.log.Errorw("failed to copy plan for next day", "error", err, "userID", userID)
			return c.Edit("Произошла ошибка при сохранении ваших планов. Пожалуйста, попробуйте позже.")
		}
		return c.Edit("Хорошо, ваши планы и время пробуждения остаются без изменений.")
	case btnUpdatePlans:
		// Update plans
		bot.stateMutex.Lock()
		bot.userStates[userID] = &UserData{State: StateAwaitingPlans}
		bot.stateMutex.Unlock()
		return c.Edit("Пожалуйста, расскажите о ваших новых планах на завтра.")
	case btnNoWish:
		bot.stateMutex.Lock()
		delete(bot.userStates, userID)
		bot.stateMutex.Unlock()
		return c.Edit("Хорошо, вы не получите пожелание завтра.")
	default:
		return c.Edit("Неизвестный выбор. Пожалуйста, попробуйте еще раз.")
	}
}

func (bot *Bot) handleNameUpdate(c tele.Context) error {
	newName := c.Text()
	userID := c.Sender().ID

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

	delete(bot.userStates, userID)
	return c.Send(fmt.Sprintf("Ваше имя успешно обновлено на %s.", newName))
}

func (bot *Bot) handleBioUpdate(c tele.Context) error {
	newBio := c.Text()
	userID := c.Sender().ID

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

	delete(bot.userStates, userID)
	return c.Send("Ваше био успешно обновлено.")
}

func (bot *Bot) handleTimezoneUpdate(c tele.Context) error {
	timeStr := c.Text()
	userID := c.Sender().ID

	// Parse the time
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return c.Send("Извините, я не смог понять это время. Пожалуйста, попробуйте еще раз, используя формат ЧЧ:ММ (например, 14:30)")
	}

	// Calculate timezone offset
	now := time.Now().UTC()
	userTime := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	tzOffset := int32(userTime.Sub(now).Minutes())

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

	delete(bot.userStates, userID)
	return c.Send("Ваш часовой пояс успешно обновлен.")
}

func (bot *Bot) handlePlansUpdate(c tele.Context) error {
	newPlans := c.Text()
	userID := c.Sender().ID

	plan, err := bot.db.GetLatestPlan(userID)
	if err != nil {
		bot.log.Errorw("failed to get latest plan", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	plan.Content = newPlans
	if err := bot.db.SavePlan(plan); err != nil {
		bot.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении ваших планов. Пожалуйста, попробуйте позже.")
	}

	delete(bot.userStates, userID)
	return c.Send("Ваши планы успешно обновлены.")
}

func (bot *Bot) handleWakeTimeUpdate(c tele.Context) error {
	wakeTimeStr := c.Text()
	userID := c.Sender().ID

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Parse the time
	localWakeTime, err := time.Parse("15:04", wakeTimeStr)
	if err != nil {
		return c.Send("Извините, я не смог понять это время. Пожалуйста, попробуйте еще раз, используя формат ЧЧ:ММ (например, 07:30)")
	}

	// Create a time.Location using the user's timezone offset
	userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)

	// Set the wake time to tomorrow in the user's timezone
	now := time.Now().In(userLoc)
	localWakeTime = time.Date(now.Year(), now.Month(), now.Day()+1, localWakeTime.Hour(), localWakeTime.Minute(), 0, 0, userLoc)

	// Convert local wake time to UTC
	utcWakeTime := localWakeTime.UTC()

	plan, err := bot.db.GetLatestPlan(userID)
	if err != nil {
		bot.log.Errorw("failed to get latest plan", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	plan.WakeAt = utcWakeTime
	if err := bot.db.SavePlan(plan); err != nil {
		bot.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашего времени пробуждения. Пожалуйста, попробуйте позже.")
	}

	delete(bot.userStates, userID)
	return c.Send(fmt.Sprintf("Ваше время пробуждения успешно обновлено на %s.", wakeTimeStr))
}

func (bot *Bot) handleNotificationTimeUpdate(c tele.Context) error {
	notificationTimeStr := c.Text()
	userID := c.Sender().ID

	user, err := bot.db.GetUser(userID)
	if err != nil {
		bot.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Parse the time
	notificationTime, err := time.Parse("15:04", notificationTimeStr)
	if err != nil {
		return c.Send("Извините, я не смог понять это время. Пожалуйста, попробуйте еще раз, используя формат ЧЧ:ММ (например, 21:00)")
	}

	// Convert to UTC
	now := time.Now().UTC()
	userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)
	notifyAtUTC := time.Date(now.Year(), now.Month(), now.Day(), notificationTime.Hour(), notificationTime.Minute(), 0, 0, userLoc).UTC()

	user.NotifyAt = notifyAtUTC
	if err := bot.db.SaveUser(user); err != nil {
		bot.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Reschedule plan reminder
	bot.schedulePlanReminder(user)

	delete(bot.userStates, userID)
	return c.Send(fmt.Sprintf("Ваше время уведомления успешно обновлено на %s.", notificationTimeStr))
}
