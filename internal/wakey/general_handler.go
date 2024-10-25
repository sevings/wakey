package wakey

import (
	"fmt"
	"net/url"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type GeneralHandler struct {
	db   *DB
	log  *zap.SugaredLogger
	name string
}

func NewGeneralHandler(db *DB, log *zap.SugaredLogger, botName string) *GeneralHandler {
	return &GeneralHandler{
		db:   db,
		log:  log,
		name: botName,
	}
}

func (gh *GeneralHandler) Actions() []string {
	return []string{btnDoNothing, btnInviteFriends, btnShowLink}
}

func (gh *GeneralHandler) HandleAction(c tele.Context, action string) error {
	inviteLink := "https://t.me/" + gh.name
	switch action {
	case btnInviteFriends:
		message := "Пригласите друзей присоединиться к нашему боту! Выберите способ:"

		inlineKeyboard := &tele.ReplyMarkup{}
		btnShowLink := inlineKeyboard.Data("Показать ссылку", btnShowLink)
		btnShareLink := inlineKeyboard.URL("Поделиться", createShareLink(inviteLink))

		inlineKeyboard.Inline(
			inlineKeyboard.Row(btnShowLink),
			inlineKeyboard.Row(btnShareLink),
		)

		return c.Edit(message, inlineKeyboard)
	case btnShowLink:
		message := fmt.Sprintf("Вот ссылка для приглашения друзей:\n\n%s\n\nПросто скопируйте и отправьте её вашим друзьям!", inviteLink)
		return c.Edit(message)
	case btnDoNothing:
		return c.Edit("Хорошо, до свидания! Если вам что-то понадобится, просто напишите мне.")
	default:
		gh.log.Errorw("unexpected action for GeneralHandler", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (gh *GeneralHandler) States() []UserState {
	return []UserState{StateSuggestActions}
}

func (gh *GeneralHandler) HandleState(c tele.Context, state UserState) error {
	switch state {
	case StateSuggestActions:
		return gh.suggestActions(c)
	default:
		gh.log.Errorw("unexpected state for GeneralHandler", "state", state)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func createShareLink(botLink string) string {
	sellingText := `Присоединяйтесь к нашему боту для планирования и улучшения продуктивности!

🌟 Что умеет наш бот:
• Помогает планировать ваш день
• Напоминает обновлять планы каждый вечер
• Позволяет обмениваться вдохновляющими пожеланиями с другими пользователями
• Доставляет мотивирующие сообщения к моменту вашего пробуждения

💪 Повысьте свою продуктивность, получайте поддержку и вдохновение каждый день!`

	encodedText := url.QueryEscape(sellingText + "\n\n" + botLink)
	return "https://t.me/share/url?url=" + encodedText
}

func (gh *GeneralHandler) suggestActions(c tele.Context) error {
	inlineKeyboard := &tele.ReplyMarkup{}

	btnShowProfile := inlineKeyboard.Data("Показать мой профиль", btnShowProfile)
	btnChangeName := inlineKeyboard.Data("Изменить имя", btnChangeName)
	btnChangeBio := inlineKeyboard.Data("Изменить био", btnChangeBio)
	btnChangeTimezone := inlineKeyboard.Data("Изменить часовой пояс", btnChangeTimezone)
	btnChangePlans := inlineKeyboard.Data("Изменить планы на завтра", btnChangePlans)
	btnChangeWakeTime := inlineKeyboard.Data("Изменить время пробуждения", btnChangeWakeTime)
	btnChangeNotifyTime := inlineKeyboard.Data("Изменить время уведомления", btnChangeNotifyTime)
	btnSendWish := inlineKeyboard.Data("Отправить пожелание", btnSendWishYes)
	btnInviteFriends := inlineKeyboard.Data("Пригласить друзей", btnInviteFriends)
	btnDoNothing := inlineKeyboard.Data("Ничего, до свидания", btnDoNothing)

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
