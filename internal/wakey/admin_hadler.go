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
		btnBanUserID,
		btnSkipBanID,
	}
}

func (ah *AdminHandler) HandleAction(c tele.Context, action string) error {
	if c.Sender().ID != ah.adm {
		return nil
	}

	if action != btnBanUserID && action != btnSkipBanID {
		ah.log.Errorw("unexpected action for AdminHandler", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}

	return ah.HandleBanCallback(c)
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
	return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
}

func (h *AdminHandler) HandleBanCallback(c tele.Context) error {
	data := strings.Split(c.Data(), "|")
	action := strings.TrimSpace(data[0])
	userIDStr := data[1]

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return c.Edit("Ошибка при обработке ID пользователя.")
	}

	user, err := h.db.GetUserByID(userID)
	if err != nil {
		h.log.Errorw("failed to get user", "error", err, "userID", userID)
		return c.Edit("Ошибка при получении информации о пользователе.")
	}

	switch action {
	case btnBanUserID:
		user.IsBanned = true
		if err := h.db.SaveUser(user); err != nil {
			h.log.Errorw("failed to ban user", "error", err, "userID", userID)
			return c.Edit("Ошибка при бане пользователя.")
		}

		// Notify the banned user
		banMessage := "Вы были забанены за нарушение правил использования бота. Вы больше не сможете отправлять или получать пожелания."
		_, err = h.api.Send(tele.ChatID(userID), banMessage)
		if err != nil {
			h.log.Errorw("failed to send ban notification to user", "error", err, "userID", userID)
		}

		return c.Edit(fmt.Sprintf("Пользователь %d забанен и уведомлен.", userID))
	case btnSkipBanID:
		return c.Edit(fmt.Sprintf("Бан пользователя %d пропущен.", userID))
	default:
		return c.Edit("Неизвестное действие.")
	}
}
