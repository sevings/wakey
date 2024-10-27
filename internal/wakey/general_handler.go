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
	return []string{btnDoNothingID, btnInviteFriendsID, btnShowLinkID}
}

func (gh *GeneralHandler) HandleAction(c tele.Context, action string) error {
	inviteLink := "https://t.me/" + gh.name
	switch action {
	case btnInviteFriendsID:
		err := c.Edit(c.Message().Text + "\n\n" + btnInviteFriendsText)
		if err != nil {
			return err
		}

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
		err := c.Edit(c.Message().Text + "\n\n" + btnShowLinkText)
		if err != nil {
			return err
		}

		message := fmt.Sprintf("Вот ссылка для приглашения друзей:\n\n%s\n\nПросто скопируйте и отправьте её вашим друзьям!", inviteLink)
		return c.Send(message)
	case btnDoNothingID:
		err := c.Edit(c.Message().Text + "\n\n" + btnDoNothingText)
		if err != nil {
			return err
		}

		return c.Send("Хорошо, до свидания! Если вам что-то понадобится, просто напишите мне.")
	default:
		gh.log.Errorw("unexpected action for GeneralHandler", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
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
