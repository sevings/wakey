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
		btnShowProfileID,
		btnChangeNameID,
		btnChangeBioID,
		btnChangeTimezoneID,
	}
}

func (ph *ProfileHandler) HandleAction(c tele.Context, action string) error {
	userID := c.Sender().ID

	switch action {
	case btnShowProfileID:
		return ph.HandleShowProfile(c)
	case btnChangeNameID:
		err := c.Edit(c.Message().Text + "\n\n" + btnChangeNameText)
		if err != nil {
			return err
		}

		ph.stateMan.SetState(userID, StateUpdatingName)
		return c.Send("Пожалуйста, введите ваше новое имя. Используйте команду /cancel для отмены.")
	case btnChangeBioID:
		err := c.Edit(c.Message().Text + "\n\n" + btnChangeBioText)
		if err != nil {
			return err
		}

		ph.stateMan.SetState(userID, StateUpdatingBio)
		return c.Send("Пожалуйста, введите ваше новое био. Используйте команду /cancel для отмены.")
	case btnChangeTimezoneID:
		err := c.Edit(c.Message().Text + "\n\n" + btnChangeTimezoneText)
		if err != nil {
			return err
		}

		ph.stateMan.SetState(userID, StateUpdatingTimezone)
		return c.Send("Пожалуйста, введите текущее время в формате ЧЧ:ММ. Используйте команду /cancel для отмены.")
	default:
		ph.log.Errorw("unexpected action for ProfileHandler", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
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
	const welcomeMessage = `Я бот, который поможет вам анализировать свое состояние и обмениваться пожеланиями с другими пользователями. Вот что я умею:

1. Ежедневно сохранять ваш статус и время пробуждения.
2. Напоминать вам о необходимости обновить статус каждый вечер.
3. Отправлять ваши сообщения другим пользователям.
4. Доставлять сообщения от других пользователей в момент вашего пробуждения.

Надеюсь, мы отлично проведем время вместе!`

	userID := c.Sender().ID

	// Check if user already exists
	user, err := ph.db.GetUserByID(userID)
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
	err = c.Send(fullMessage)
	if err != nil {
		return err
	}

	return c.Send("Теперь давайте начнем регистрацию. Как вас зовут? Можно указать настоящее имя или любое прозвище.")
}

func (ph *ProfileHandler) HandleShowProfile(c tele.Context) error {
	err := c.Edit(c.Message().Text + "\n\n" + btnShowProfileText)
	if err != nil {
		return err
	}

	userID := c.Sender().ID

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка при загрузке вашего профиля. Пожалуйста, попробуйте позже.")
	}

	plan, err := ph.db.GetLatestPlan(userID)
	if err != nil && err != ErrNotFound {
		ph.log.Errorw("failed to load latest plan", "error", err)
		return c.Send("Извините, произошла ошибка при загрузке вашего статуса. Пожалуйста, попробуйте позже.")
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
		"Время уведомления: %s\n"+
		"Время пробуждения: %s\n",
		user.Name, user.Bio, user.Tz/60, localNotifyTime, localWakeTime)

	if plan != nil {
		profileMsg += fmt.Sprintf("Текущий статус: %s", plan.Content)
	} else {
		profileMsg += "Текущий статус: Не установлен"
	}

	ph.stateMan.SetState(userID, StateSuggestActions)
	return c.Send(profileMsg)
}

func (ph *ProfileHandler) HandleNameInput(c tele.Context) error {
	const msg = "Теперь, пожалуйста, расскажите немного о себе. " +
		"Можете написать, кем работаете или на кого учитесь, " +
		"как любите проводить свободное время, о чем мечтаете. " +
		"Что угодно, что поможет другим лучше понять вас как человека и вашу жизнь."

	userID := c.Sender().ID
	userData, _ := ph.stateMan.GetUserData(userID)
	userData.Name = c.Text()
	ph.stateMan.SetUserData(userID, userData)
	ph.stateMan.SetState(userID, StateAwaitingBio)
	return c.Send("Приятно познакомиться, " + userData.Name + "! " + msg)
}

func (ph *ProfileHandler) HandleNameUpdate(c tele.Context) error {
	userID := c.Sender().ID
	newName := c.Text()

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Name = newName
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.SetState(userID, StateSuggestActions)
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

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Bio = newBio
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.SetState(userID, StateSuggestActions)
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

	ph.stateMan.SetState(userID, StateAwaitingNotificationTime)
	return c.Send("Отлично! Теперь укажите, в какое время вы хотели бы получать напоминание обновить статус? (Используйте формат ЧЧ:ММ или отправьте 'отключить', чтобы отключить уведомления)")
}

func (ph *ProfileHandler) HandleTimezoneUpdate(c tele.Context) error {
	userID := c.Sender().ID

	tzOffset, err := getTimeZoneOffset(c)
	if err != nil {
		return c.Send(err.Error())
	}

	user, err := ph.db.GetUserByID(userID)
	if err != nil {
		ph.log.Errorw("failed to load user", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user.Tz = tzOffset
	if err := ph.db.SaveUser(user); err != nil {
		ph.log.Errorw("failed to save user", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашей информации. Пожалуйста, попробуйте позже.")
	}

	ph.stateMan.SetState(userID, StateSuggestActions)
	return c.Send("Ваш часовой пояс успешно обновлен.")
}
