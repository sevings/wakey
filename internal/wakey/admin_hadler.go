package wakey

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type AdminHandler struct {
	db       *DB
	stateMan *StateManager
	api      BotAPI
	adm      int64
	log      *zap.SugaredLogger
}

func NewAdminHandler(db *DB, api BotAPI, stateMan *StateManager, log *zap.SugaredLogger, adminID int64, maxToxic int16) *AdminHandler {
	ah := &AdminHandler{
		db:       db,
		stateMan: stateMan,
		api:      api,
		adm:      adminID,
		log:      log,
	}

	// Subscribe to toxicity updates
	toxicCh, _ := db.SubscribeToToxicity(100)
	go ah.monitorToxicity(toxicCh, maxToxic)

	// Subscribe to wish state updates
	stateCh, _ := db.SubscribeToStateUpdates(100)
	go ah.monitorWishStates(stateCh)

	return ah
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

	user, err := ah.handleAdminAction(c)
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
		StateNotifyAll,
		StateWaitingForNotification,
	}
}

func (ah *AdminHandler) HandleState(c tele.Context, state UserState) error {
	if c.Sender().ID != ah.adm {
		return nil
	}

	switch state {
	case StateNotifyAll:
		return ah.HandleNotifyAll(c)
	case StateWaitingForNotification:
		return ah.handleNotification(c)
	default:
		ah.log.Errorw("unexpected state for AdminHandler", "state", state)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (h *AdminHandler) handleAdminAction(c tele.Context) (*User, error) {
	data := strings.Split(c.Data(), "|")
	if len(data) != 2 {
		return nil, fmt.Errorf("invalid data format")
	}

	// Parse and validate user ID
	userIDStr := data[1]
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

func (ah *AdminHandler) HandleNotifyAll(c tele.Context) error {
	ah.stateMan.SetState(ah.adm, StateWaitingForNotification)
	return c.Send("Пожалуйста, отправьте текст уведомления, которое нужно разослать всем пользователям. Используйте /cancel для отмены.")
}

func (ah *AdminHandler) handleNotification(c tele.Context) error {
	message := c.Text()
	if message == "" {
		return c.Send("Текст уведомления не может быть пустым. Попробуйте еще раз или используйте /cancel для отмены.")
	}

	users, err := ah.db.GetAllUsers()
	if err != nil {
		ah.log.Errorw("failed to get users for notification", "error", err)
		return c.Send("Ошибка при получении списка пользователей.")
	}

	successCount := 0
	failCount := 0

	for _, user := range users {
		if user.IsBanned {
			continue
		}

		_, err := ah.api.Send(tele.ChatID(user.ID), message)
		if err != nil {
			ah.log.Warnw("failed to send notification to user",
				"error", err,
				"userID", user.ID)
			failCount++
		} else {
			successCount++
		}
	}

	return c.Send(fmt.Sprintf(
		"Уведомление отправлено:\n"+
			"✅ Успешно: %d\n"+
			"❌ С ошибкой: %d",
		successCount,
		failCount,
	))
}

func (ah *AdminHandler) monitorToxicity(ch <-chan *Wish, threshold int16) {
	for wish := range ch {
		if !wish.Toxicity.Valid {
			continue
		}

		if wish.Toxicity.Int16 >= threshold {
			ah.notifyAdminAboutToxicWish(wish)
		}
	}
}

func (ah *AdminHandler) notifyAdminAboutToxicWish(wish *Wish) {
	// Create inline keyboard
	inlineKeyboard := &tele.ReplyMarkup{}
	btnWarn := inlineKeyboard.Data(btnWarnUserText, btnWarnUserID, fmt.Sprintf("%d", wish.FromID))
	btnBan := inlineKeyboard.Data(btnBanUserText, btnBanUserID, fmt.Sprintf("%d", wish.FromID))
	btnSkip := inlineKeyboard.Data(btnSkipBanText, btnSkipBanID, fmt.Sprintf("%d", wish.FromID))
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnWarn),
		inlineKeyboard.Row(btnBan),
		inlineKeyboard.Row(btnSkip),
	)

	message := fmt.Sprintf(
		"⚠️ Обнаружено токсичное сообщение\n\n"+
			"От пользователя: %d\n"+
			"Уровень токсичности: %d%%\n"+
			"Текст: %s",
		wish.FromID,
		wish.Toxicity.Int16,
		wish.Content,
	)

	_, err := ah.api.Send(tele.ChatID(ah.adm), message, inlineKeyboard)
	if err != nil {
		ah.log.Errorw("failed to notify admin about toxic wish",
			"error", err,
			"wishID", wish.ID,
			"fromID", wish.FromID,
			"toxicity", wish.Toxicity.Int16)
	}
}

func (ah *AdminHandler) monitorWishStates(ch <-chan *Wish) {
	for wish := range ch {
		if wish.State == WishStateReported {
			ah.notifyAdminAboutReportedWish(wish)
		}
	}
}

func (ah *AdminHandler) notifyAdminAboutReportedWish(wish *Wish) {
	// Create inline keyboard
	inlineKeyboard := &tele.ReplyMarkup{}
	btnWarn := inlineKeyboard.Data(btnWarnUserText, btnWarnUserID, fmt.Sprintf("%d", wish.FromID))
	btnBan := inlineKeyboard.Data(btnBanUserID, btnBanUserID, fmt.Sprintf("%d", wish.FromID))
	btnSkip := inlineKeyboard.Data(btnSkipBanText, btnSkipBanID, fmt.Sprintf("%d", wish.FromID))
	inlineKeyboard.Inline(
		inlineKeyboard.Row(btnWarn),
		inlineKeyboard.Row(btnBan),
		inlineKeyboard.Row(btnSkip),
	)

	message := fmt.Sprintf(
		"🚫 Жалоба на сообщение\n\n"+
			"От пользователя: %d\n"+
			"Уровень токсичности: %d%%\n"+
			"Текст: %s",
		wish.FromID,
		wish.Toxicity.Int16,
		wish.Content,
	)

	_, err := ah.api.Send(tele.ChatID(ah.adm), message, inlineKeyboard)
	if err != nil {
		ah.log.Errorw("failed to notify admin about reported wish",
			"error", err,
			"wishID", wish.ID,
			"fromID", wish.FromID)
	}
}
