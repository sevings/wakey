package wakey

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type AdminHandler struct {
	db  *DB
	api BotAPI
	adm int64
	log *zap.SugaredLogger
}

func NewAdminHandler(db *DB, log *zap.SugaredLogger) *AdminHandler {
	return &AdminHandler{
		db:  db,
		log: log,
	}
}

func (ah *AdminHandler) SetAPI(api BotAPI) {
	ah.api = api
}

func (ah *AdminHandler) SetAdminID(adminID int64) {
	ah.adm = adminID
}

func (ah *AdminHandler) Actions() []string {
	return []string{
		btnWarnUserID,
		btnBanUserID,
		btnSkipBanID,
	}
}

func (ah *AdminHandler) HandleAction(c tele.Context, action string) error {
	if c.Sender().ID != ah.adm {
		return nil
	}

	user, err := ah.handleAdminAction(c, action)
	if user == nil {
		return err
	}

	switch action {
	case btnWarnUserID:
		return ah.HandleWarn(c, user)
	case btnBanUserID:
		return ah.handleBan(c, user)
	case btnSkipBanID:
		return ah.handleSkip(c, user)
	default:
		ah.log.Errorw("unexpected action for AdminHandler", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (ah *AdminHandler) States() []UserState {
	return []UserState{
		// Admin handler doesn't have any specific states,
		// but we need to implement this method to satisfy the interface
	}
}

func (ah *AdminHandler) HandleState(c tele.Context, state UserState) error {
	// Admin handler doesn't handle any specific states,
	// but we need to implement this method to satisfy the interface
	ah.log.Errorw("unexpected state for AdminHandler", "state", state)
	return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
}

func (h *AdminHandler) handleAdminAction(c tele.Context, action string) (*User, error) {
	data := strings.Split(c.Data(), "|")
	if len(data) != 2 {
		return nil, fmt.Errorf("invalid data format")
	}

	userIDStr := data[1]

	// Update message with button text
	var btnText string
	switch action {
	case btnBanUserID:
		btnText = btnBanUserText
	case btnSkipBanID:
		btnText = btnSkipBanText
	case btnWarnUserID:
		btnText = btnWarnUserText
	}

	err := c.Edit(c.Message().Text + "\n\n" + btnText)
	if err != nil {
		return nil, err
	}

	// Parse and validate user ID
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		h.log.Errorw("failed to parse user id", "error", err, "userID", userIDStr)
		return nil, c.Send("Ошибка при обработке ID пользователя.")
	}

	// Get user info
	user, err := h.db.GetUserByID(userID)
	if err != nil {
		h.log.Errorw("failed to get user", "error", err, "userID", userID)
		return nil, c.Send("Ошибка при получении информации о пользователе.")
	}

	return user, nil
}

func (h *AdminHandler) HandleWarn(c tele.Context, user *User) error {
	warningMessage := "⚠️ Предупреждение. Ваше сообщение было помечено как неуместное. " +
		"Пожалуйста, будьте вежливы и уважительны к другим пользователям. " +
		"Повторные нарушения могут привести к бану."

	_, err := h.api.Send(tele.ChatID(user.ID), warningMessage)
	if err != nil {
		h.log.Errorw("failed to send warning to user", "error", err, "userID", user.ID)
		return c.Send("Ошибка при отправке предупреждения пользователю.")
	}

	return c.Send(fmt.Sprintf("Предупреждение отправлено пользователю %d.", user.ID))
}

func (h *AdminHandler) handleBan(c tele.Context, user *User) error {
	user.IsBanned = true
	if err := h.db.SaveUser(user); err != nil {
		h.log.Errorw("failed to ban user", "error", err, "userID", user.ID)
		return c.Send("Ошибка при бане пользователя.")
	}

	// Notify the banned user
	banMessage := "Вы были забанены за нарушение правил использования бота."
	_, err := h.api.Send(tele.ChatID(user.ID), banMessage)
	if err != nil {
		h.log.Errorw("failed to send ban notification to user", "error", err, "userID", user.ID)
	}

	return c.Send(fmt.Sprintf("Пользователь %d забанен и уведомлен.", user.ID))
}

func (h *AdminHandler) handleSkip(c tele.Context, user *User) error {
	return c.Send(fmt.Sprintf("Бан пользователя %d пропущен.", user.ID))
}
