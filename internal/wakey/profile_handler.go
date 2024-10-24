package wakey

import (
	"errors"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type ProfileHandler struct {
	db       *DB
	stateMan *StateManager
	log      *zap.SugaredLogger
}

func NewProfileHandler(db *DB, stateMan *StateManager, log *zap.SugaredLogger) *ProfileHandler {
	return &ProfileHandler{
		db:       db,
		stateMan: stateMan,
		log:      log,
	}
}

func (ph *ProfileHandler) Actions() []string {
	return []string{
		btnShowProfile,
		btnChangeName,
		btnChangeBio,
		btnChangeTimezone,
	}
}

func (ph *ProfileHandler) HandleAction(c tele.Context, action string) error {
	userID := c.Sender().ID

	switch action {
	case btnShowProfile:
		return ph.HandleShowProfile(c)
	case btnChangeName:
		ph.stateMan.SetState(userID, StateUpdatingName)
		return c.Edit("Пожалуйста, введите ваше новое имя.")
	case btnChangeBio:
		ph.stateMan.SetState(userID, StateUpdatingBio)
		return c.Edit("Пожалуйста, введите ваше новое био.")
	case btnChangeTimezone:
		ph.stateMan.SetState(userID, StateUpdatingTimezone)
		return c.Edit("Пожалуйста, введите текущее время в формате ЧЧ:ММ.")
	default:
		ph.log.Errorw("unexpected action for ProfileHandler", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (ph *ProfileHandler) States() []UserState {
	return []UserState{
		StateRegistrationStart,
		StateAwaitingName,
		StateAwaitingBio,
		StateAwaitingTime,
		StateUpdatingName,
		StateUpdatingBio,
		StateUpdatingTimezone,
	}
}

func (ph *ProfileHandler) HandleState(c tele.Context, state UserState) error {
	switch state {
	case StateRegistrationStart:
		return ph.HandleStart(c)
	case StateAwaitingName:
		return ph.HandleNameInput(c)
	case StateUpdatingName:
		return ph.HandleNameUpdate(c)
	case StateAwaitingBio:
		return ph.HandleBioInput(c)
	case StateUpdatingBio:
		return ph.HandleBioUpdate(c)
	case StateAwaitingTime:
		return ph.HandleTimeInput(c)
	case StateUpdatingTimezone:
		return ph.HandleTimezoneUpdate(c)
	default:
		ph.log.Errorw("unexpected state for ProfileHandler", "state", state)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (ph *ProfileHandler) HandleStart(c tele.Context) error {
	const welcomeMessage = `Я бот, который поможет вам планировать ваш день и обмениваться пожеланиями с другими пользователями. Вот что я умею:

1. Сохранять ваши ежедневные планы и время пробуждения.
2. Напоминать вам о необходимости обновить планы каждый вечер.
3. Позволять вам отправлять пожелания другим пользователям.
4. Доставлять пожелания от других пользователей в момент вашего пробуждения.

Надеюсь, мы отлично проведем время вместе!`

	userID := c.Sender().ID

	// Check if user already exists
	user, err := ph.db.GetUser(userID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		ph.log.Errorw("failed to check user existence", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}
	if err != ErrNotFound {
		ph.stateMan.ClearState(userID)
		welcomeBack := fmt.Sprintf("С возвращением, %s! Вы уже зарегистрированы.", user.Name)
		fullMessage := welcomeBack + "\n\n" + welcomeMessage
		return c.Send(fullMessage)
	}

	// Start registration process
	ph.stateMan.SetState(userID, StateAwaitingName)
	fullMessage := "Добро пожаловать! Давайте зарегистрируем вас. Но сначала, позвольте рассказать о моих возможностях.\n\n"
	fullMessage += welcomeMessage
	fullMessage += "\n\nТеперь давайте начнем регистрацию. Как вас зовут?"
	return c.Send(fullMessage)
}

func (ph *ProfileHandler) HandleShowProfile(c tele.Context) error {
	userID := c.Sender().ID

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Edit("Извините, произошла ошибка при загрузке вашего профиля. Пожалуйста, попробуйте позже.")
	}

	plan, err := ph.db.GetLatestPlan(userID)
	if err != nil && err != ErrNotFound {
		ph.log.Errorw("failed to load latest plan", "error", err)
		return c.Edit("Извините, произошла ошибка при загрузке ваших планов. Пожалуйста, попробуйте позже.")
	}

	userLoc := time.FixedZone("User Timezone", int(user.Tz)*60)
	localWakeTime := "Не установлено"
	localNotifyTime := "Отключено"

	if !user.NotifyAt.IsZero() {
		localNotifyTime = user.NotifyAt.In(userLoc).Format("15:04")
	}

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

	return c.Edit(profileMsg)
}

func (ph *ProfileHandler) HandleNameInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := ph.stateMan.GetUserData(userID)
	userData.Name = c.Text()
	ph.stateMan.SetUserData(userID, userData)
	ph.stateMan.SetState(userID, StateAwaitingBio)
	return c.Send("Приятно познакомиться, " + userData.Name + "! Теперь, пожалуйста, расскажите немного о себе (краткое био).")
}

func (ph *ProfileHandler) HandleNameUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newName := c.Text()

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Name = newName
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.ClearState(userID)
	return c.Send(fmt.Sprintf("Ваше имя успешно обновлено на %s.", newName))
}

func (ph *ProfileHandler) HandleBioInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := ph.stateMan.GetUserData(userID)
	userData.Bio = c.Text()
	ph.stateMan.SetUserData(userID, userData)
	ph.stateMan.SetState(userID, StateAwaitingTime)
	return c.Send("Отлично! Наконец, скажите, который сейчас у вас час? (Пожалуйста, используйте формат ЧЧ:ММ)")
}

func (ph *ProfileHandler) HandleBioUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newBio := c.Text()

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Bio = newBio
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.ClearState(userID)
	return c.Send("Ваше био успешно обновлено.")
}

func getTimeZoneOffset(c tele.Context) (int32, error) {
	timeStr := c.Text()

	userTime, err := parseTime(timeStr, 0) // Use 0 as initial timezone offset
	if err != nil {
		return 0, err
	}

	// Calculate timezone offset
	tzOffset := int32(userTime.Sub(time.Now().UTC()).Minutes())
	tzOffset = int32(math.Round(float64(tzOffset)/15) * 15)
	return tzOffset, nil
}

func (ph *ProfileHandler) HandleTimeInput(c tele.Context) error {
	userID := c.Sender().ID

	tzOffset, err := getTimeZoneOffset(c)
	if err != nil {
		return c.Send(err.Error())
	}
	userData, _ := ph.stateMan.GetUserData(userID)

	// Create new user in database
	user := User{
		ID:   userID,
		Name: userData.Name,
		Bio:  userData.Bio,
		Tz:   tzOffset,
	}
	if err := ph.db.CreateUser(&user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	// Registration complete
	ph.stateMan.SetState(userID, StateAwaitingPlans)
	return c.Send("Отлично! Теперь расскажите о ваших планах на завтра.")
}

func (ph *ProfileHandler) HandleTimezoneUpdate(c tele.Context) error {
	userID := c.Sender().ID

	tzOffset, err := getTimeZoneOffset(c)
	if err != nil {
		return c.Send(err.Error())
	}

	user, err := ph.db.GetUser(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Tz = tzOffset
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.ClearState(userID)
	return c.Send("Ваш часовой пояс успешно обновлен.")
}
