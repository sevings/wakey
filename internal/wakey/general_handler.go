package wakey

import (
	"fmt"
	"net/url"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type GeneralHandler struct {
	db       *DB
	stateMan *StateManager
	log      *zap.SugaredLogger
	name     string
}

func NewGeneralHandler(db *DB, stateMan *StateManager, log *zap.SugaredLogger, botName string) *GeneralHandler {
	return &GeneralHandler{
		db:       db,
		stateMan: stateMan,
		log:      log,
		name:     botName,
	}
}

func (gh *GeneralHandler) Actions() []string {
	return []string{btnDoNothingID, btnInviteFriendsID, btnShowLinkID}
}

func (gh *GeneralHandler) HandleAction(c tele.Context, action string) error {
	inviteLink := "https://t.me/" + gh.name
	switch action {
	case btnInviteFriendsID:
		message := "Пригласите друзей присоединиться к нашему боту! Выберите способ:"

		inlineKeyboard := &tele.ReplyMarkup{}
		btnShowLink := inlineKeyboard.Data(btnShowLinkText, btnShowLinkID)
		btnShareLink := inlineKeyboard.URL(btnShareLinkText, createShareLink(inviteLink))

		inlineKeyboard.Inline(
			inlineKeyboard.Row(btnShowLink),
			inlineKeyboard.Row(btnShareLink),
		)

		return c.Send(message, inlineKeyboard)
	case btnShowLinkID:
		message := fmt.Sprintf("Вот ссылка для приглашения друзей:\n\n%s\n\nПросто скопируйте и отправьте её вашим друзьям!", inviteLink)
		return c.Send(message)
	case btnDoNothingID:
		return c.Send("Хорошо, до свидания! Если вам что-то понадобится, просто напишите мне.")
	default:
		gh.log.Errorw("unexpected action for GeneralHandler", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (gh *GeneralHandler) States() []UserState {
	return []UserState{StateSuggestActions, StateCancelAction, StatePrintStats}
}

func (gh *GeneralHandler) HandleState(c tele.Context, state UserState) error {
	switch state {
	case StateSuggestActions:
		return gh.suggestActions(c)
	case StateCancelAction:
		return gh.cancelAction(c)
	case StatePrintStats:
		return gh.printStats(c)
	default:
		gh.log.Errorw("unexpected state for GeneralHandler", "state", state)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func createShareLink(botLink string) string {
	sellingText := `Присоединяйтесь к нашему боту — повысьте свою осознанность, получайте поддержку и вдохновение каждый день!

Что умеет наш бот:
• Напоминает останавливаться и анализировать свое состояние
• Позволяет обмениваться вдохновляющими пожеланиями с другими пользователями
• Доставляет поддерживающие сообщения к моменту вашего пробуждения
`

	encodedText := url.QueryEscape(sellingText + "\n\n" + botLink)
	return "https://t.me/share/url?url=" + encodedText
}

func (gh *GeneralHandler) suggestActions(c tele.Context) error {
	userID := c.Sender().ID
	gh.stateMan.ClearState(userID)

	_, err := gh.db.GetUserByID(userID)
	if err != nil {
		if err == ErrNotFound {
			return c.Send("Похоже, вы еще не зарегистрированы. Пожалуйста, используйте команду /start чтобы начать процесс регистрации.")
		}
		gh.log.Errorw("failed to get user", "error", err, "userID", userID)
		return c.Send("Произошла ошибка при проверке вашего профиля. Пожалуйста, попробуйте позже.")
	}

	inlineKeyboard := &tele.ReplyMarkup{}

	btnShowProfile := inlineKeyboard.Data(btnShowProfileText, btnShowProfileID)
	btnChangeName := inlineKeyboard.Data(btnChangeNameText, btnChangeNameID)
	btnChangeBio := inlineKeyboard.Data(btnChangeBioText, btnChangeBioID)
	btnChangeTimezone := inlineKeyboard.Data(btnChangeTimezoneText, btnChangeTimezoneID)
	btnChangePlans := inlineKeyboard.Data(btnChangePlansText, btnChangePlansID)
	btnChangeWakeTime := inlineKeyboard.Data(btnChangeWakeTimeText, btnChangeWakeTimeID)
	btnChangeNotifyTime := inlineKeyboard.Data(btnChangeNotifyTimeText, btnChangeNotifyTimeID)
	btnSendWish := inlineKeyboard.Data(btnSendWishYesText, btnSendWishYesID)
	btnInviteFriends := inlineKeyboard.Data(btnInviteFriendsText, btnInviteFriendsID)
	btnDoNothing := inlineKeyboard.Data(btnDoNothingText, btnDoNothingID)

	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnShowProfile),
		inlineKeyboard.Row(btnChangeName),
		inlineKeyboard.Row(btnChangeBio),
		inlineKeyboard.Row(btnChangeTimezone),
		inlineKeyboard.Row(btnChangePlans),
		inlineKeyboard.Row(btnChangeWakeTime),
		inlineKeyboard.Row(btnChangeNotifyTime),
		inlineKeyboard.Row(btnSendWish),
		inlineKeyboard.Row(btnInviteFriends),
		inlineKeyboard.Row(btnDoNothing),
	)

	return c.Send("Что бы вы хотели сделать?", inlineKeyboard)
}

func (gh *GeneralHandler) cancelAction(c tele.Context) error {
	userID := c.Sender().ID
	state, exists := gh.stateMan.GetState(userID)
	if exists && state != StateNone {
		err := c.Send("Действие отменено.")
		if err != nil {
			return err
		}
	}

	return gh.suggestActions(c)
}

func (gh *GeneralHandler) printStats(c tele.Context) error {
	stats, err := gh.db.GetStats()
	if err != nil {
		gh.log.Errorw("failed to get stats", "error", err)
		return c.Send("Извините, не удалось получить статистику. Пожалуйста, попробуйте позже.")
	}

	message := fmt.Sprintf(`📊 *Статистика бота*

*Общая статистика:*
• Всего пользователей: %d
• Всего статусов: %d
• Всего сообщений: %d
• Понравившихся сообщений: %d (%.2f%%)

*За последние 7 дней:*
• Новых пользователей: %d
• Активных пользователей: %d
• Среднее число статусов в день: %.2f
• Среднее число сообщений в день: %.2f
• Понравившихся сообщений: %d (%.2f%%)`,
		stats.TotalUsers,
		stats.TotalPlans,
		stats.TotalWishes,
		stats.TotalLikedWishes,
		stats.LikedWishesPercent,
		stats.NewUsersLast7Days,
		stats.ActiveUsersLast7Days,
		stats.AvgPlansLast7Days,
		stats.AvgWishesLast7Days,
		stats.LikedWishesLast7Days,
		stats.LikedWishesLast7DaysPercent,
	)

	return c.Send(message, tele.ModeMarkdown)
}
