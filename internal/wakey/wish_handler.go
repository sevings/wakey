package wakey

import (
	"errors"
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
	log      *zap.SugaredLogger
}

func NewWishHandler(db *DB, api BotAPI, wishSched Scheduler, stateMan *StateManager, log *zap.SugaredLogger) *WishHandler {
	wh := &WishHandler{
		db:       db,
		api:      api,
		stateMan: stateMan,
		log:      log,
	}

	wishSched.SetJobFunc(wh.SendWishes)

	return wh
}

func (wh *WishHandler) Actions() []string {
	return []string{
		btnWishLikeID,
		btnWishDislikeID,
		btnWishReportID,
		btnSendWishYesID,
		btnSendWishNoID,
	}
}

func getButtonWishID(c tele.Context) (uint64, error) {
	data := strings.Split(c.Data(), "|")
	if len(data) != 2 {
		return 0, errors.New("Неверный формат данных.")
	}
	wishID, err := strconv.ParseUint(data[1], 10, 64)
	if err != nil {
		return 0, errors.New("Неверный ID сообщения.")
	}
	return wishID, nil
}

func (wh *WishHandler) HandleAction(c tele.Context, action string) error {
	switch action {
	case btnSendWishYesID:
		return wh.HandleSendWishResponse(c)
	case btnSendWishNoID:
		return wh.HandleSendWishNo(c)
	case btnWishDislikeID:
		return wh.HandleWishDislike(c)
	case btnWishLikeID, btnWishReportID:
		wishID, err := getButtonWishID(c)
		if err != nil {
			return c.Send(err.Error())
		}
		wish, err := wh.db.GetWishByID(uint(wishID))
		if err != nil {
			return c.Send("Не удалось найти сообщение.")
		}
		if action == btnWishLikeID {
			return wh.HandleWishLike(c, wish)
		} else {
			return wh.HandleWishReport(c, wish)
		}
	default:
		wh.log.Errorw("unexpected action for WishHandler", "action", action)
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
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
		return c.Send("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
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

	// Update wish state
	err = wh.db.UpdateWishState(wish.ID, WishStateLiked)
	if err != nil {
		wh.log.Errorw("failed to update wish state", "error", err, "wishID", wish.ID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Send message to the wish author
	thanksMsg := fmt.Sprintf("Пользователю %s понравилось ваше сообщение.", user.Name)
	_, err = wh.api.Send(tele.ChatID(wish.FromID), thanksMsg)
	if err != nil {
		wh.log.Errorw("failed to send thanks message", "error", err, "userID", wish.FromID)
	}

	return c.Send("Благодарность за сообщение отправлена.")
}

func (wh *WishHandler) HandleWishDislike(c tele.Context) error {
	wishID, err := getButtonWishID(c)
	if err != nil {
		return c.Send(err.Error())
	}

	// Update wish state
	err = wh.db.UpdateWishState(uint(wishID), WishStateDisliked)
	if err != nil {
		wh.log.Errorw("failed to update wish state", "error", err, "wishID", wishID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	return c.Send("Спасибо за ваш ответ.")
}

func (wh *WishHandler) HandleWishReport(c tele.Context, wish *Wish) error {
	err := wh.db.UpdateWishState(wish.ID, WishStateReported)
	if err != nil {
		wh.log.Errorw("failed to update wish state", "error", err, "wishID", wish.ID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	return c.Send("Жалоба на сообщение отправлена.")
}

func (wh *WishHandler) HandleSendWishResponse(c tele.Context) error {
	err := c.Send("Хорошо, давайте отправим сообщение!")
	if err != nil {
		return err
	}

	return wh.FindUserForWish(c)
}

func (wh *WishHandler) HandleSendWishNo(c tele.Context) error {
	wh.stateMan.SetState(c.Sender().ID, StateSuggestActions)
	return c.Send("Хорошо, может быть в следующий раз!")
}

func (wh *WishHandler) FindUserForWish(c tele.Context) error {
	senderID := c.Sender().ID

	plan, err := wh.db.FindPlanForWish(senderID)
	if err != nil {
		if err == ErrNotFound {
			wh.stateMan.SetState(senderID, StateSuggestActions)
			return c.Send("К сожалению, сейчас нет пользователей, которым можно отправить сообщение.")
		}
		wh.log.Errorw("failed to find user for wish", "error", err)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	user, err := wh.db.GetUserByID(plan.UserID)
	if err != nil {
		wh.log.Errorw("failed to get user", "error", err, "userID", plan.UserID)
		return c.Send("Извините, произошла ошибка. Пожалуйста, попробуйте позже.")
	}

	// Set user state and data
	userData := &UserData{
		State:        StateAwaitingWish,
		TargetPlanID: plan.ID,
	}
	wh.stateMan.SetUserData(senderID, userData)

	const msg = "Напишите сообщение этому пользователю.\n\n" +
		"Можете поддержать, пожелать что-нибудь доброе, поделиться мыслями. " +
		"Уважайте друг друга. " +
		"Постарайтесь не давать советов и оценок, если об этом явно не попросили.\n\n" +
		"Если совсем не хочется ничего писать, используйте команду /cancel."
	err = c.Send(msg)
	if err != nil {
		return err
	}

	return c.Send(fmt.Sprintf("%s\n\n%s\n\n%s", user.Name, user.Bio, plan.Content))
}

func (wh *WishHandler) HandleWishInput(c tele.Context) error {
	userID := c.Sender().ID
	wishText := c.Text()
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
		return c.Send("Извините, время для отправки сообщения этому пользователю истекло. Пожалуйста, попробуйте отправить новое сообщение.")
	}

	wish := &Wish{
		FromID:  userID,
		PlanID:  userData.TargetPlanID,
		Content: wishText,
	}

	if err := wh.db.SaveWish(wish); err != nil {
		wh.log.Errorw("failed to save wish", "error", err)
		return c.Send("Извините, произошла ошибка при сохранении вашего сообщения. Пожалуйста, попробуйте позже.")
	}

	wh.stateMan.SetState(userID, StateSuggestActions)
	return c.Send("Спасибо! Ваше сообщение отправлено и будет доставлено пользователю в запланированное время.")
}

func (wh *WishHandler) SendWishes(id JobID) {
	userID := int64(id)

	// Get user
	user, err := wh.db.GetUserByID(userID)
	if err != nil {
		wh.log.Errorw("failed to get user", "error", err, "userID", userID)
		return
	}

	// Skip if user is banned
	if user.IsBanned {
		wh.log.Infow("skipping wishes for banned user", "userID", userID)
		return
	}

	// Get all new wishes for user's plans
	wishes, err := wh.db.GetNewWishesByUserID(userID)
	if err != nil {
		wh.log.Errorw("failed to get new wishes", "error", err, "userID", userID)
		return
	}

	if len(wishes) == 0 {
		wh.log.Infow("no new wishes found for user", "userID", userID)
		return
	}

	// Send greeting
	_, err = wh.api.Send(tele.ChatID(userID), "Доброе утро! Вот, что вам написали:")
	if err != nil {
		wh.log.Errorw("failed to send greeting", "error", err, "userID", userID)
	}

	// Send each wish to the recipient
	for _, wish := range wishes {
		// Create inline keyboard
		inlineKeyboard := &tele.ReplyMarkup{}
		btnLike := inlineKeyboard.Data(btnWishLikeText, btnWishLikeID, fmt.Sprintf("%d", wish.ID))
		btnDislike := inlineKeyboard.Data(btnWishDislikeText, btnWishDislikeID, fmt.Sprintf("%d", wish.ID))
		btnReport := inlineKeyboard.Data(btnWishReportText, btnWishReportID, fmt.Sprintf("%d", wish.ID))
		inlineKeyboard.Inline(
			inlineKeyboard.Row(btnLike),
			inlineKeyboard.Row(btnDislike),
			inlineKeyboard.Row(btnReport),
		)

		// Send message with inline keyboard
		_, err = wh.api.Send(tele.ChatID(userID), wish.Content, inlineKeyboard)
		if err != nil {
			wh.log.Errorw("failed to send wish", "error", err, "userID", userID, "wishID", wish.ID)
			continue
		}

		// Update wish state to sent
		err = wh.db.UpdateWishState(wish.ID, WishStateSent)
		if err != nil {
			wh.log.Errorw("failed to update wish state", "error", err, "wishID", wish.ID)
		}
	}
}
