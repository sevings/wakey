package wakey

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type WishHandler struct {
	db       *DB
	api      BotAPI
	stateMan *StateManager
	adm      int64
	log      *zap.SugaredLogger
}

func NewWishHandler(db *DB, wishSched Scheduler, stateMan *StateManager, log *zap.SugaredLogger) *WishHandler {
	wh := &WishHandler{
		db:       db,
		stateMan: stateMan,
		log:      log,
	}

	wishSched.SetJobFunc(wh.SendWishes)

	return wh
}

func (wh *WishHandler) SetAPI(api BotAPI) {
	wh.api = api
}

func (wh *WishHandler) SetAdminID(adminID int64) {
	wh.adm = adminID
}

func (wh *WishHandler) Actions() []string {
	return []string{
		btnWishLike,
		btnWishDislike,
		btnWishReport,
		btnSendWishYes,
		btnSendWishNo,
	}
}

func (wh *WishHandler) HandleAction(c tele.Context, action string) error {
	switch action {
	case btnSendWishYes:
		return wh.HandleSendWishResponse(c)
	case btnSendWishNo:
		return wh.HandleSendWishNo(c)
	case btnWishDislike:
		return wh.HandleWishDislike(c)
	case btnWishLike, btnWishReport:
		data := strings.Split(c.Data(), "|")
		if len(data) != 2 {
			return c.Send("Неверный формат данных.")
		}
		wishID, err := strconv.ParseUint(data[1], 10, 64)
		if err != nil {
			return c.Send("Неверный ID пожелания.")
		}
		wish, err := wh.db.GetWishByID(uint(wishID))
		if err != nil {
			return c.Send("Не удалось найти пожелание.")
		}
		if action == btnWishLike {
			return wh.HandleWishLike(c, wish)
		} else {
			return wh.HandleWishReport(c, wish)
		}
	default:
		wh.log.Errorw("unexpected action for WishHandler", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (wh *WishHandler) States() []UserState {
	return []UserState{
		StateAwaitingWish,
	}
}

func (wh *WishHandler) HandleState(c tele.Context, state UserState) error {
	switch state {
	case StateAwaitingWish:
		return wh.HandleWishInput(c)
	default:
		wh.log.Errorw("unexpected state for WishHandler", "state", state)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (wh *WishHandler) HandleWishLike(c tele.Context, wish *Wish) error {
	plan, err := wh.db.GetPlanByID(wish.PlanID)
	if err != nil {
		wh.log.Errorw("failed to get plan", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user, err := wh.db.GetUserByID(plan.UserID)
	if err != nil {
		wh.log.Errorw("failed to get user", "error", err, "userID", plan.UserID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Send message to the wish author
	thanksMsg := fmt.Sprintf("Пользователю %s понравилось ваше пожелание.", user.Name)
	_, err = wh.api.Send(tele.ChatID(wish.FromID), thanksMsg)
	if err != nil {
		wh.log.Errorw("failed to send thanks message", "error", err, "userID", wish.FromID)
	}

	return wh.RemoveWishKeyboard(c)
}

func (wh *WishHandler) HandleWishDislike(c tele.Context) error {
	return wh.RemoveWishKeyboard(c)
}

func (wh *WishHandler) HandleWishReport(c tele.Context, wish *Wish) error {
	if wh.adm != 0 {
		reportMsg := fmt.Sprintf("Жалоба на пожелание:\n\nАвтор ID: %d\nТекст пожелания: %s", wish.FromID, wish.Content)

		inlineKeyboard := &tele.ReplyMarkup{}
		btnBan := inlineKeyboard.Data("Забанить", btnBanUser, fmt.Sprintf("%d", wish.FromID))
		btnSkip := inlineKeyboard.Data("Пропустить", btnSkipBan, fmt.Sprintf("%d", wish.FromID))
		inlineKeyboard.Inline(
			inlineKeyboard.Row(btnBan, btnSkip),
		)

		_, err := wh.api.Send(tele.ChatID(wh.adm), reportMsg, inlineKeyboard)
		if err != nil {
			wh.log.Errorw("failed to send report to admin", "error", err)
		}
	}

	return wh.RemoveWishKeyboard(c)
}

func (wh *WishHandler) RemoveWishKeyboard(c tele.Context) error {
	err := c.Edit(c.Message().Text)
	if err != nil {
		wh.log.Errorw("failed to remove wish keyboard", "error", err)
		return c.Send("Произошла ошибка при обработке вашего ответа.")
	}

	return c.Send("Спасибо за ваш ответ!")
}

func (wh *WishHandler) HandleSendWishResponse(c tele.Context) error {
	err := c.Edit("Хорошо, давайте отправим пожелание!")
	if err != nil {
		wh.log.Errorw("failed to remove send wish keyboard", "error", err)
	}

	return wh.FindUserForWish(c)
}

func (wh *WishHandler) HandleSendWishNo(c tele.Context) error {
	err := c.Edit("Хорошо, может быть в следующий раз!")
	if err != nil {
		wh.log.Errorw("failed to remove send wish keyboard", "error", err)
		return c.Send("Произошла ошибка при обработке вашего ответа.")
	}

	wh.stateMan.SetState(c.Sender().ID, StateSuggestActions)
	return nil
}

func (wh *WishHandler) FindUserForWish(c tele.Context) error {
	senderID := c.Sender().ID

	plan, err := wh.db.FindPlanForWish(senderID)
	if err != nil {
		if err == ErrNotFound {
			return c.Send("К сожалению, сейчас нет пользователей, которым можно отправить пожелание.")
		}
		wh.log.Errorw("failed to find user for wish", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user, err := wh.db.GetUserByID(plan.UserID)
	if err != nil {
		wh.log.Errorw("failed to get user", "error", err, "userID", plan.UserID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	userInfo := fmt.Sprintf("Имя: %s\nО себе: %s\nПланы: %s",
		user.Name, user.Bio, plan.Content)

	// Set user state and data
	userData := &UserData{
		State:        StateAwaitingWish,
		TargetPlanID: plan.ID,
	}
	wh.stateMan.SetUserData(senderID, userData)

	return c.Send(fmt.Sprintf("Отправьте ваше пожелание для этого пользователя:\n\n%s", userInfo))
}

func (wh *WishHandler) HandleWishInput(c tele.Context) error {
	userID := c.Sender().ID
	userData, _ := wh.stateMan.GetUserData(userID)
	if userData == nil {
		return c.Send("Извините, произошла ошибка. Пожалуйста, начните процесс заново.")
	}

	plan, err := wh.db.GetPlanByID(userData.TargetPlanID)
	if err != nil {
		wh.log.Errorw("failed to get plan", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	if time.Now().UTC().Sub(plan.OfferedAt) > time.Hour {
		wh.stateMan.ClearState(userID)
		return c.Send("Извините, время для отправки пожелания истекло. Пожалуйста, попробуйте отправить новое пожелание.")
	}

	wish := &Wish{
		FromID:  userID,
		PlanID:  userData.TargetPlanID,
		Content: c.Text(),
	}

	if err := wh.db.SaveWish(wish); err != nil {
		wh.log.Errorw("failed to save wish", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашего пожелания. Пожалуйста, попробуйте позже.")
	}

	wh.stateMan.SetState(userID, StateSuggestActions)
	return c.Send("Спасибо! Ваше пожелание отправлено и будет доставлено пользователю в запланированное время.")
}

func (wh *WishHandler) SendWishes(id JobID) {
	planID := uint(id)
	plan, err := wh.db.GetPlanByID(planID)
	if err != nil {
		wh.log.Errorw("failed to get plan", "error", err, "planID", planID)
		return
	}

	// Check if the recipient is banned
	recipient, err := wh.db.GetUserByID(plan.UserID)
	if err != nil {
		wh.log.Errorw("failed to get recipient", "error", err, "userID", plan.UserID)
		return
	}

	if recipient.IsBanned {
		wh.log.Infow("skipping wishes for banned user", "userID", plan.UserID)
		return
	}

	// Get all wishes for this plan
	wishes, err := wh.db.GetWishesByPlanID(planID)
	if err != nil {
		wh.log.Errorw("failed to get wishes", "error", err, "planID", planID)
		return
	}

	if len(wishes) == 0 {
		wh.log.Infow("no wishes found for plan", "planID", planID)
		return
	}

	// Send each wish to the recipient
	for _, wish := range wishes {
		message := fmt.Sprintf("Доброе утро! Вот пожелание для вас:\n\n%s", wish.Content)

		// Create inline keyboard
		inlineKeyboard := &tele.ReplyMarkup{}
		btnLike := inlineKeyboard.Data("Спасибо, понравилось", btnWishLike, fmt.Sprintf("%d", wish.ID))
		btnDislike := inlineKeyboard.Data("Ну такое…", btnWishDislike, fmt.Sprintf("%d", wish.ID))
		btnReport := inlineKeyboard.Data("Пожаловаться", btnWishReport, fmt.Sprintf("%d", wish.ID))
		inlineKeyboard.Inline(
			inlineKeyboard.Row(btnLike),
			inlineKeyboard.Row(btnDislike),
			inlineKeyboard.Row(btnReport),
		)

		// Send message with inline keyboard
		_, err = wh.api.Send(tele.ChatID(plan.UserID), message, inlineKeyboard)
		if err != nil {
			wh.log.Errorw("failed to send wish", "error", err, "userID", plan.UserID, "wishID", wish.ID)
		}
	}
}
