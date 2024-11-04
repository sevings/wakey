package wakey

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type PlanHandler struct {
	api       BotAPI
	db        *DB
	stateMan  *StateManager
	planSched Scheduler
	wishSched Scheduler
	log       *zap.SugaredLogger
}

func NewPlanHandler(db *DB, api BotAPI, planSched, wishSched Scheduler, stateMan *StateManager, log *zap.SugaredLogger) *PlanHandler {
	ph := &PlanHandler{
		api:       api,
		db:        db,
		stateMan:  stateMan,
		planSched: planSched,
		wishSched: wishSched,
		log:       log,
	}

	planSched.SetJobFunc(ph.notifyAboutPlansUpdate)
	ph.ScheduleAllNotifications()

	return ph
}

func (ph *PlanHandler) Actions() []string {
	return []string{
		btnChangePlansID,
		btnChangeWakeTimeID,
		btnChangeNotifyTimeID,
		btnKeepPlansID,
		btnUpdatePlansID,
		btnNoWishID,
	}
}

func (ph *PlanHandler) HandleAction(c tele.Context, action string) error {
	userID := c.Sender().ID

	switch action {
	case btnChangePlansID:
		ph.stateMan.SetState(userID, StateUpdatingPlans)
		return ph.askAboutPlans(c)
	case btnChangeWakeTimeID:
		ph.stateMan.SetState(userID, StateUpdatingWakeTime)
		return c.Send("Пожалуйста, введите новое время пробуждения в формате ЧЧ:ММ. Используйте команду /cancel для отмены.")
	case btnChangeNotifyTimeID:
		ph.stateMan.SetState(userID, StateUpdatingNotificationTime)
		return c.Send("Пожалуйста, введите новое время уведомления в формате ЧЧ:ММ. " +
			"Если вы хотите отключить уведомления, отправьте 'отключить'.\n" +
			"Или используйте команду /cancel для отмены.")
	case btnKeepPlansID:
		plan, err := ph.db.CopyPlanForNextDay(userID)
		if err != nil {
			ph.log.Errorw("failed to copy plan for next day", "error", err, "userID", userID)
			return c.Send("Произошла ошибка при сохранении вашего статуса. Пожалуйста, попробуйте позже.")
		}
		ph.scheduleWishSend(plan)
		err = c.Send("Хорошо, ваши статус и время пробуждения остаются без изменений.")
		if err != nil {
			return err
		}

		return ph.askAboutWish(c)
	case btnUpdatePlansID:
		ph.stateMan.SetState(userID, StateAwaitingPlans)
		return ph.askAboutPlans(c)
	case btnNoWishID:
		ph.stateMan.ClearState(userID)
		return c.Send("Хорошо, завтра вы не получите сообщение от другого пользователя.")
	default:
		ph.log.Errorw("unexpected action for PlanHandler", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (ph *PlanHandler) States() []UserState {
	return []UserState{
		StateAwaitingPlans,
		StateAwaitingWakeTime,
		StateAwaitingNotificationTime,
		StateUpdatingPlans,
		StateUpdatingWakeTime,
		StateUpdatingNotificationTime,
	}
}

func (ph *PlanHandler) HandleState(c tele.Context, state UserState) error {
	switch state {
	case StateAwaitingPlans:
		return ph.HandlePlansInput(c)
	case StateUpdatingPlans:
		return ph.HandlePlansUpdate(c)
	case StateAwaitingWakeTime:
		return ph.HandleWakeTimeInput(c)
	case StateUpdatingWakeTime:
		return ph.HandleWakeTimeUpdate(c)
	case StateAwaitingNotificationTime:
		return ph.HandleNotificationTimeInput(c)
	case StateUpdatingNotificationTime:
		return ph.HandleNotificationTimeUpdate(c)
	default:
		ph.log.Errorw("unexpected state for PlanHandler", "state", state)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (ph *PlanHandler) schedulePlanReminder(user *User) {
	if user.NotifyAt.IsZero() {
		ph.planSched.Cancel(JobID(user.ID))
		return
	}

	now := time.Now().UTC()
	nextNotification := user.NotifyAt

	for nextNotification.Before(now) {
		nextNotification = nextNotification.Add(24 * time.Hour)
	}

	ph.planSched.Schedule(nextNotification, JobID(user.ID))
	ph.log.Infow("scheduled notification", "userID", user.ID, "notifyAt", nextNotification)
}

func (ph *PlanHandler) scheduleWishSend(plan *Plan) {
	ph.wishSched.Schedule(plan.WakeAt, JobID(plan.UserID))
	ph.log.Infow("scheduled wish", "userID", plan.UserID, "wakeAt", plan.WakeAt)
}

func (ph *PlanHandler) askAboutPlans(c tele.Context) error {
	const caption = "Пожалуйста, расскажите кратко о своем состоянии в текущий момент. " +
		"Можете написать о своих чувствах, свои мысли, о сегодняшнем дне, " +
		"о планах на завтра — все, что вам сейчас важно. Форма свободная, " +
		"главное — внимание на себя.\n\n" +
		"Можно, хотя и не обязательно, использовать прикрепленные списки чувств и потребностей.\n" +
		"Или отправьте команду /cancel для отмены."

	album := tele.Album{
		&tele.Photo{File: tele.FromDisk("./data/feelings.png"), Caption: caption},
		&tele.Photo{File: tele.FromDisk("./data/needs.png")},
	}

	return c.SendAlbum(album)
}

func (ph *PlanHandler) askAboutWish(c tele.Context) error {
	userID := c.Sender().ID
	userData, exists := ph.stateMan.GetUserData(userID)
	if !exists || !userData.AskAboutWish {
		ph.stateMan.SetState(userID, StateSuggestActions)
		return nil
	}

	if userData.State == StateNone {
		ph.stateMan.ClearState(userID)
	} else {
		userData.AskAboutWish = false
		ph.stateMan.SetUserData(userID, userData)
	}

	// Ask if the user wants to send a wish
	inlineKeyboard := &tele.ReplyMarkup{}
	btnYes := inlineKeyboard.Data(btnSendWishYesText, btnSendWishYesID)
	btnNo := inlineKeyboard.Data(btnSendWishNoText, btnSendWishNoID)
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnYes),
		inlineKeyboard.Row(btnNo),
	)

	return c.Send("Хотите отправить сообщение другому пользователю?", inlineKeyboard)
}

func (ph *PlanHandler) HandlePlansInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := ph.stateMan.GetUserData(userID)
	userData.Plans = c.Text()
	userData.AskAboutWish = true
	ph.stateMan.SetUserData(userID, userData)
	ph.stateMan.SetState(userID, StateAwaitingWakeTime)
	return c.Send("Отлично! Теперь скажите, во сколько вы планируете проснуться завтра? (Используйте формат ЧЧ:ММ)")
}

func (ph *PlanHandler) HandleWakeTimeInput(c tele.Context) error {
	userID := c.Sender().ID
	wakeTimeStr := c.Text()

	// Load user to get timezone
	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	utcWakeTime, err := parseTime(wakeTimeStr, user.Tz)
	if err != nil {
		return c.Send(err.Error())
	}

	userData, _ := ph.stateMan.GetUserData(userID)

	// Save plan to database
	plan := &Plan{
		UserID:  userID,
		Content: userData.Plans,
		WakeAt:  utcWakeTime,
	}

	if err := ph.db.SavePlan(plan); err != nil {
		ph.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}
	ph.scheduleWishSend(plan)

	err = c.Send("Ваше время пробуждения успешно обновлено.")
	if err != nil {
		return err
	}

	return ph.askAboutWish(c)
}

func (ph *PlanHandler) HandlePlansUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newPlans := c.Text()

	now := time.Now().UTC()
	plan, err := ph.db.CopyPlanForNextDay(userID)
	if err != nil {
		if err == ErrNotFound {
			// Create a new plan if no existing plan is found
			plan = &Plan{
				UserID: userID,
				WakeAt: now.Add(24 * time.Hour), // Set default wake time to 24 hours from now
			}
		} else {
			ph.log.Errorw("failed to get latest plan", "error", err)
			return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
		}
	}
	plan.Content = newPlans

	for plan.WakeAt.Before(now) {
		plan.WakeAt = plan.WakeAt.Add(24 * time.Hour)
	}

	if err := ph.db.SavePlan(plan); err != nil {
		ph.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашего статуса. Пожалуйста, попробуйте позже.")
	}
	ph.scheduleWishSend(plan)

	err = c.Send("Ваш статус успешно обновлен.")
	if err != nil {
		return err
	}

	return ph.askAboutWish(c)
}

func (ph *PlanHandler) HandleWakeTimeUpdate(c tele.Context) error {
	userID := c.Sender().ID
	wakeTimeStr := c.Text()

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	utcWakeTime, err := parseTime(wakeTimeStr, user.Tz)
	if err != nil {
		return c.Send(err.Error())
	}

	plan, err := ph.db.CopyPlanForNextDay(userID)
	if err != nil {
		if err == ErrNotFound {
			// Create a new plan if no existing plan is found
			plan = &Plan{
				UserID:  userID,
				Content: "Планы не указаны", // Default content
			}
		} else {
			ph.log.Errorw("failed to get latest plan", "error", err)
			return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
		}
	}
	plan.WakeAt = utcWakeTime

	if err := ph.db.SavePlan(plan); err != nil {
		ph.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашего времени пробуждения. Пожалуйста, попробуйте позже.")
	}

	ph.scheduleWishSend(plan)
	err = c.Send(fmt.Sprintf("Ваше время пробуждения успешно обновлено на %s.", wakeTimeStr))
	if err != nil {
		return err
	}

	return ph.askAboutWish(c)
}

func (ph *PlanHandler) notifyAboutPlansUpdate(id JobID) {
	userID := int64(id)
	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err, "userID", userID)
		return
	}

	// Get the latest plan
	plan, err := ph.db.GetLatestPlan(userID)
	if err != nil && err != ErrNotFound {
		ph.log.Errorw("failed to get latest plan", "error", err, "userID", userID)
		return
	}

	// Show previous plans first
	previousPlansMsg := "Пора рассказать о вашем текущем состоянии!"
	if err == ErrNotFound || plan == nil {
		previousPlansMsg += "\n\nУ вас пока нет сохраненного статуса."
	} else {
		// Convert UTC wake time to user's timezone
		userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)
		localWakeTime := plan.WakeAt.In(userLoc)
		previousPlansMsg += fmt.Sprintf(
			"\n\nВаш текущий статус: %s"+
				"\n\nВремя пробуждения: %s",
			plan.Content,
			localWakeTime.Format("15:04"))
	}

	// Send previous plans message first
	_, err = ph.api.Send(tele.ChatID(userID), previousPlansMsg)
	if err != nil {
		ph.log.Errorw("failed to send previous plans", "error", err, "userID", userID)
		return
	}

	// Create inline keyboard
	inlineKeyboard := &tele.ReplyMarkup{}
	btnKeep := inlineKeyboard.Data(btnKeepPlansText, btnKeepPlansID)
	btnChangeAll := inlineKeyboard.Data(btnUpdatePlansText, btnUpdatePlansID)
	btnChangePlans := inlineKeyboard.Data(btnChangePlansText, btnChangePlansID)
	btnChangeTime := inlineKeyboard.Data(btnChangeWakeTimeText, btnChangeWakeTimeID)
	btnNoWish := inlineKeyboard.Data(btnNoWishText, btnNoWishID)
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnKeep),
		inlineKeyboard.Row(btnChangeAll),
		inlineKeyboard.Row(btnChangePlans),
		inlineKeyboard.Row(btnChangeTime),
		inlineKeyboard.Row(btnNoWish),
	)

	_, err = ph.api.Send(tele.ChatID(userID), "Что вы хотите сделать?", inlineKeyboard)
	if err != nil {
		ph.log.Errorw("failed to send plan reminder", "error", err, "userID", userID)
	}

	userData, exists := ph.stateMan.GetUserData(userID)
	if exists {
		userData.AskAboutWish = true
	} else {
		userData = &UserData{
			AskAboutWish: true,
		}
	}
	ph.stateMan.SetUserData(userID, userData)

	// Reschedule for the next day
	ph.schedulePlanReminder(user)
}

func (ph *PlanHandler) HandleNotificationTimeInput(c tele.Context) error {
	userID := c.Sender().ID
	notificationTimeStr := c.Text()

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err, "userID", userID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	if strings.ToLower(notificationTimeStr) == "отключить" {
		user.NotifyAt = time.Time{} // Set to zero time to indicate notifications are disabled
	} else {
		notifyAtUTC, err := parseTime(notificationTimeStr, user.Tz)
		if err != nil {
			return c.Send(err.Error())
		}
		user.NotifyAt = notifyAtUTC
	}

	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.schedulePlanReminder(user)
	ph.stateMan.ClearState(userID)
	// Inform user about notification settings
	var notificationMsg string
	if user.NotifyAt.IsZero() {
		notificationMsg = "Уведомления о статусе отключены."
	} else {
		notificationMsg = fmt.Sprintf("Я буду напоминать вам обновить статус каждый день в %s.", notificationTimeStr)
	}

	err = c.Send(fmt.Sprintf("Отлично! Регистрация завершена. %s", notificationMsg))
	if err != nil {
		return err
	}

	ph.stateMan.SetState(userID, StateAwaitingPlans)
	return ph.askAboutPlans(c)
}

func (ph *PlanHandler) HandleNotificationTimeUpdate(c tele.Context) error {
	userID := c.Sender().ID
	notificationTimeStr := c.Text()

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	if strings.ToLower(notificationTimeStr) == "выключить" {
		user.NotifyAt = time.Time{} // Set to zero time to indicate notifications are disabled
	} else {
		notifyAtUTC, err := parseTime(notificationTimeStr, user.Tz)
		if err != nil {
			return c.Send(err.Error())
		}
		user.NotifyAt = notifyAtUTC
	}

	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.schedulePlanReminder(user)
	ph.stateMan.SetState(userID, StateSuggestActions)

	if user.NotifyAt.IsZero() {
		return c.Send("Уведомления о статусе отключены.")
	}

	return c.Send(fmt.Sprintf("Ваше время уведомления успешно обновлено на %s.", notificationTimeStr))
}

func (ph *PlanHandler) ScheduleAllNotifications() {
	users, err := ph.db.GetAllUsers()
	if err != nil {
		ph.log.Errorw("failed to get all users", "error", err)
		return
	}

	cnt := 0
	for _, user := range users {
		if !user.IsBanned && !user.NotifyAt.IsZero() {
			ph.schedulePlanReminder(user)
			cnt++
		}
	}

	ph.log.Infow("Scheduled notifications", "users", cnt)

	plans, err := ph.db.GetFuturePlans()
	if err != nil {
		ph.log.Errorw("failed to get future plans", "error", err)
		return
	}

	for _, plan := range plans {
		ph.scheduleWishSend(&plan)
	}

	ph.log.Infow("Scheduled plans", "users", len(plans))
}
