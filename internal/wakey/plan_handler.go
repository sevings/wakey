package wakey

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type PlanHandler struct {
	api       BotAPI
	db        *DB
	stateMan  *StateManager
	planSched Scheduler
	log       *zap.SugaredLogger
}

func NewPlanHandler(db *DB, planSched Scheduler, stateMan *StateManager, log *zap.SugaredLogger) *PlanHandler {
	ph := &PlanHandler{
		db:        db,
		stateMan:  stateMan,
		planSched: planSched,
		log:       log,
	}

	planSched.SetJobFunc(ph.AskAboutPlans)

	return ph
}

func (ph *PlanHandler) SetAPI(api BotAPI) {
	ph.api = api
}

func (ph *PlanHandler) Actions() []string {
	return []string{
		btnChangePlans,
		btnChangeWakeTime,
		btnChangeNotifyTime,
		btnKeepPlans,
		btnUpdatePlans,
		btnNoWish,
	}
}

func (ph *PlanHandler) HandleAction(c tele.Context, action string) error {
	userID := c.Sender().ID

	switch action {
	case btnChangePlans:
		ph.stateMan.SetState(userID, StateUpdatingPlans)
		return c.Edit("Пожалуйста, введите ваши новые планы на завтра.")
	case btnChangeWakeTime:
		ph.stateMan.SetState(userID, StateUpdatingWakeTime)
		return c.Edit("Пожалуйста, введите новое время пробуждения в формате ЧЧ:ММ.")
	case btnChangeNotifyTime:
		ph.stateMan.SetState(userID, StateUpdatingNotificationTime)
		return c.Edit("Пожалуйста, введите новое время уведомления в формате ЧЧ:ММ.")
	case btnKeepPlans:
		err := ph.db.CopyPlanForNextDay(userID)
		if err != nil {
			ph.log.Errorw("failed to copy plan for next day", "error", err, "userID", userID)
			return c.Edit("Произошла ошибка при сохранении ваших планов. Пожалуйста, попробуйте позже.")
		}
		ph.stateMan.ClearState(userID)
		return c.Edit("Хорошо, ваши планы и время пробуждения остаются без изменений.")
	case btnUpdatePlans:
		ph.stateMan.SetState(userID, StateAwaitingPlans)
		return c.Edit("Пожалуйста, расскажите о ваших новых планах на завтра.")
	case btnNoWish:
		ph.stateMan.ClearState(userID)
		return c.Edit("Хорошо, вы не получите пожелание завтра.")
	default:
		ph.log.Errorw("unexpected action for PlanHandler", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
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
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (ph *PlanHandler) schedulePlanReminder(user *User) {
	now := time.Now().UTC()
	nextNotification := user.NotifyAt

	if nextNotification.Before(now) {
		nextNotification = nextNotification.Add(24 * time.Hour)
	}

	ph.planSched.Schedule(nextNotification, JobID(user.ID))
}

func (ph *PlanHandler) HandlePlansInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := ph.stateMan.GetUserData(userID)
	userData.Plans = c.Text()
	ph.stateMan.SetUserData(userID, userData)
	ph.stateMan.SetState(userID, StateAwaitingWakeTime)
	return c.Send("Отлично! Теперь скажите, во сколько вы планируете проснуться завтра? (Используйте формат ЧЧ:ММ)")
}

func (ph *PlanHandler) HandleWakeTimeInput(c tele.Context) error {
	userID := c.Sender().ID
	wakeTimeStr := c.Text()

	// Load user to get timezone
	user, err := ph.db.GetUser(userID)
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

	ph.stateMan.ClearState(userID)

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

func (ph *PlanHandler) HandlePlansUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newPlans := c.Text()

	plan, err := ph.db.GetLatestPlan(userID)
	if err != nil {
		if err == ErrNotFound {
			// Create a new plan if no existing plan is found
			plan = &Plan{
				UserID: userID,
				WakeAt: time.Now().UTC().Add(24 * time.Hour), // Set default wake time to 24 hours from now
			}
		} else {
			ph.log.Errorw("failed to get latest plan", "error", err)
			return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
		}
	}
	plan.Content = newPlans

	if err := ph.db.SavePlan(plan); err != nil {
		ph.log.Errorw("failed to save plan", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении ваших планов. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.ClearState(userID)
	return c.Send("Ваши планы успешно обновлены.")
}

func (ph *PlanHandler) HandleWakeTimeUpdate(c tele.Context) error {
	userID := c.Sender().ID
	wakeTimeStr := c.Text()

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	utcWakeTime, err := parseTime(wakeTimeStr, user.Tz)
	if err != nil {
		return c.Send(err.Error())
	}

	plan, err := ph.db.GetLatestPlan(userID)
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

	ph.stateMan.ClearState(userID)
	return c.Send(fmt.Sprintf("Ваше время пробуждения успешно обновлено на %s.", wakeTimeStr))
}

func (ph *PlanHandler) AskAboutPlans(id JobID) {
	userID := int64(id)
	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err, "userID", userID)
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

	_, err = ph.api.Send(tele.ChatID(userID), "Пора рассказать о ваших планах на завтра! Что вы хотите сделать?", inlineKeyboard)
	if err != nil {
		ph.log.Errorw("failed to send plan reminder", "error", err, "userID", userID)
	}

	// Reschedule for the next day
	ph.schedulePlanReminder(user)
}

func (ph *PlanHandler) HandleNotificationTimeInput(c tele.Context) error {
	userID := c.Sender().ID
	notificationTimeStr := c.Text()

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err, "userID", userID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	notifyAtUTC, err := parseTime(notificationTimeStr, user.Tz)
	if err != nil {
		return c.Send(err.Error())
	}
	user.NotifyAt = notifyAtUTC

	// Save user to database
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Schedule asking about plans
	ph.schedulePlanReminder(user)

	// Registration complete
	ph.stateMan.ClearState(userID)
	return c.Send(fmt.Sprintf("Отлично! Регистрация завершена. Я буду напоминать вам о планах каждый день в %s.", notificationTimeStr))
}

func (ph *PlanHandler) HandleNotificationTimeUpdate(c tele.Context) error {
	userID := c.Sender().ID
	notificationTimeStr := c.Text()

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	notifyAtUTC, err := parseTime(notificationTimeStr, user.Tz)
	if err != nil {
		return c.Send(err.Error())
	}
	user.NotifyAt = notifyAtUTC

	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Reschedule plan reminder
	ph.schedulePlanReminder(user)

	ph.stateMan.ClearState(userID)
	return c.Send(fmt.Sprintf("Ваше время уведомления успешно обновлено на %s.", notificationTimeStr))
}
